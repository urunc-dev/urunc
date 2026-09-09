// Copyright (c) 2023-2026, Nubificus LTD
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hypervisors

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	QemuVmm    VmmType = "qemu"
	QemuBinary string  = "qemu-system-"
)

type Qemu struct {
	binaryPath string
	binary     string
	vhost      bool
}

func (q *Qemu) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

func (q *Qemu) Stop(pid int) error {
	return killProcess(pid)
}

func (q *Qemu) Ok() error {
	return nil
}

// UsesKVM returns a bool value depending on if the monitor uses KVM
func (q *Qemu) UsesKVM() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (q *Qemu) SupportsSharedfs(_ string) bool {
	return true
}

func (q *Qemu) Path() string {
	return q.binaryPath
}

func (q *Qemu) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	qemuMem := BytesToStringMB(args.MemSizeB)
	exArgs := []string{
		q.binaryPath,
		"-m", qemuMem + "M",
		"-L", "/usr/share/qemu", // Set the path for qemu bios/data
		"-cpu", "host", // Choose CPU
		"-enable-kvm",      // Enable KVM to use CPU virt extensions
		"-display", "none", // Disable graphic output
		"-vga", "none",
		"-serial", "stdio",
		"-monitor", "null",
	}
	// server,nowait lets QEMU boot without waiting for a client to connect.
	exArgs = append(exArgs, "-qmp", "unix:"+ResolveSocketPath(args)+",server,nowait")

	if args.VCPUs > 0 {
		exArgs = append(exArgs, "-smp", strconv.FormatUint(uint64(args.VCPUs), 10))
	}

	if args.Seccomp {
		// Enable Seccomp in QEMU. The sandbox options below:
		//   obsolete=deny          - Deny Obsolete system calls
		//   elevateprivileges=deny - Deny set*uid|gid system calls
		//   spawn=deny             - Deny *fork and execve
		//   resourcecontrol=deny   - Deny process affinity and scheduler priority
		exArgs = append(exArgs,
			"--sandbox", "on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny",
		)
	}

	// TODO: explore alternative implementations
	if runtime.GOARCH == "arm64" {
		exArgs = append(exArgs, "-M", "virt")
	}

	exArgs = append(exArgs, "-kernel", args.UnikernelPath)
	if args.Net.TapDev != "" {
		netcli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
		if len(netcli) == 0 {
			netdevArg := fmt.Sprintf("tap,id=net0,script=no,downscript=no,ifname=%s", args.Net.TapDev)
			if q.vhost {
				netdevArg += ",vhost=on"
			}
			exArgs = append(exArgs, "-netdev", netdevArg)

			devType := "virtio-net-pci"
			if runtime.GOARCH == "arm64" {
				devType = "virtio-net-device"
			}
			netCliDev := fmt.Sprintf("%s,netdev=net0,host_mtu=%d,mac=%s", devType, args.Net.MTU, args.Net.MAC)
			exArgs = append(exArgs, "-device", netCliDev)
		} else {
			exArgs = append(exArgs, netcli...)
		}
	} else {
		exArgs = append(exArgs, "-nic", "none")
	}

	blockArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range blockArgs {
		if blockArg.ID != "" && blockArg.Path != "" {
			devArg := fmt.Sprintf("virtio-blk-pci,serial=%s,drive=%s,scsi=off", blockArg.ID, blockArg.ID)
			drvArg := fmt.Sprintf("format=raw,if=none,id=%s,file=%s", blockArg.ID, blockArg.Path)
			exArgs = append(exArgs, "-device", devArg, "-drive", drvArg)
		} else if len(blockArg.ExactArgs) > 0 {
			exArgs = append(exArgs, blockArg.ExactArgs...)
		}
	}

	if args.InitrdPath != "" {
		exArgs = append(exArgs, "-initrd", args.InitrdPath)
	}

	switch args.Sharedfs.Type {
	case "9pfs":
		fsdevArg := fmt.Sprintf("local,id=rootfs9p,security_model=none,path=%s", args.Sharedfs.Path)
		exArgs = append(exArgs, "-fsdev", fsdevArg, "-device", "virtio-9p-pci,fsdev=rootfs9p,mount_tag=fs0")
	case "virtiofs":
		objArg := fmt.Sprintf("memory-backend-file,id=mem,size=%sM,mem-path=/tmp,share=on", qemuMem)
		exArgs = append(exArgs,
			"-object", objArg,
			"-numa", "node,memdev=mem",
			"-chardev", "socket,id=char0,path=/tmp/vhostqemu",
			"-device", "vhost-user-fs-pci,queue-size=1024,chardev=char0,tag=fs0",
		)
	default:
		// Nothing to add
	}

	extraMonArgs := ukernel.MonitorCli()
	if extraMonArgs.ExtraInitrd != "" {
		exArgs = append(exArgs, "-initrd", extraMonArgs.ExtraInitrd)
	}
	exArgs = append(exArgs, extraMonArgs.OtherArgs...)

	if args.VAccelType == "vsock" {
		vsockArg := fmt.Sprintf("vhost-vsock-pci,id=vhost-vsock-pci0,guest-cid=%d", args.VSockDevID)
		exArgs = append(exArgs, "-device", vsockArg)
	}

	exArgs = append(exArgs, "-append", args.Command)
	return exArgs, nil
}

func (q *Qemu) PreExec(_ types.ExecArgs) error {
	return nil
}

// QemuSession is a QEMU child process started with its CPUs frozen (-S) and
// controlled over its QMP socket.
type QemuSession struct {
	cmd    *exec.Cmd
	client *qmpClient
}

// SpawnPausedVMM starts QEMU with its CPUs frozen (-S). It must run after
// changeRoot, so the QMP socket stays inside the monitor rootfs.
func (q *Qemu) SpawnPausedVMM(args types.ExecArgs, ukernel types.Unikernel, uid, gid uint32) (*QemuSession, error) {
	execCmd, err := q.BuildExecCmd(args, ukernel)
	if err != nil {
		return nil, err
	}
	execCmd = append(execCmd, "-S")

	socketPath := ResolveSocketPath(args)
	// QEMU binds this path, and a leftover file makes the bind fail.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove stale socket %q: %w", socketPath, err)
	}

	cmd := exec.Command(execCmd[0], execCmd[1:]...) //nolint: gosec
	cmd.Env = args.Environment
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if uid != 0 || gid != 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: uid, Gid: gid},
		}
	}
	vmmLog.WithField("command", execCmd).Debug("starting QEMU as a paused supervised child")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start qemu: %w", err)
	}

	client, err := connectQMP(socketPath, 5*time.Second)
	if err != nil {
		s := &QemuSession{cmd: cmd}
		s.Kill()
		return nil, err
	}
	return &QemuSession{cmd: cmd, client: client}, nil
}

// Resume unfreezes the guest CPUs; the guest boots from this moment.
func (s *QemuSession) Resume() error {
	vmmLog.Debug("api boot: sending QMP cont")
	return s.client.execute("cont")
}

// Kill terminates the child process and reaps it.
func (s *QemuSession) Kill() {
	if s.client != nil {
		s.client.close()
	}
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
}

// Supervise forwards signals to QEMU and exits with its exit code once it
// exits. It does not return on success.
func (s *QemuSession) Supervise() error {
	// Forward the signals that stop a container. SIGKILL cannot be caught,
	// so this process dies at once and leaves QEMU running.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig, ok := <-sigCh
		if !ok {
			return
		}
		if sg, ok := sig.(syscall.Signal); ok {
			_ = s.cmd.Process.Signal(sg)
		}
	}()

	waitErr := s.cmd.Wait()
	signal.Stop(sigCh)
	close(sigCh)

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			vmmLog.WithError(waitErr).Error("qemu exited with an unexpected error")
			exitCode = 1
		}
	}
	os.Exit(exitCode)
	return nil // unreachable
}
