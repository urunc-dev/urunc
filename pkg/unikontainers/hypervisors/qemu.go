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
	"strings"
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
	cmdString := q.binaryPath + " -m " + qemuMem + "M"
	cmdString += " -L /usr/share/qemu"                                  // Set the path for qemu bios/data
	cmdString += " -cpu host"                                           // Choose CPU
	cmdString += " -enable-kvm"                                         // Enable KVM to use CPU virt extensions
	cmdString += " -display none -vga none -serial stdio -monitor null" // Disable graphic output
	// server,nowait lets QEMU boot without waiting for a client to connect.
	cmdString += " -qmp unix:" + ResolveSocketPath(args) + ",server,nowait"

	if args.VCPUs > 0 {
		cmdString += fmt.Sprintf(" -smp %d", args.VCPUs)
	}

	if args.Seccomp {
		// Enable Seccomp in QEMU
		cmdString += " --sandbox on"
		// Allow or Deny Obsolete system calls
		cmdString += ",obsolete=deny"
		// Allow or Deny set*uid|gid system calls
		cmdString += ",elevateprivileges=deny"
		// Allow or Deny *fork and execve
		cmdString += ",spawn=deny"
		// Allow or Deny process affinity and schedular priority
		cmdString += ",resourcecontrol=deny"
	}

	// TODO: Check if this check causes any performance drop
	// or explore alternative implementations
	if runtime.GOARCH == "arm64" {
		machineType := " -M virt"
		cmdString += machineType
	}

	cmdString += " -kernel " + args.UnikernelPath
	if args.Net.TapDev != "" {
		netcli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
		if netcli == "" {
			netcli += " -netdev tap,id=net0,script=no,downscript=no,ifname="
			netcli += args.Net.TapDev
			if q.vhost {
				netcli += ",vhost=on"
			}
			netcli += fmt.Sprintf(" %s,host_mtu=%d,mac=%s", getVirtioNetArg(), args.Net.MTU, args.Net.MAC)
		}
		cmdString += netcli
	} else {
		cmdString += " -nic none"
	}
	blockArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range blockArgs {
		blockCli := blockArg.ExactArgs
		if blockCli == "" && blockArg.ID != "" && blockArg.Path != "" {
			blockCli1 := fmt.Sprintf(" -device virtio-blk-pci,serial=%s,drive=%s,scsi=off", blockArg.ID, blockArg.ID)
			blockCli2 := fmt.Sprintf(" -drive format=raw,if=none,id=%s,file=%s", blockArg.ID, blockArg.Path)
			blockCli = blockCli1 + blockCli2
		}
		cmdString += blockCli
	}
	if args.InitrdPath != "" {
		cmdString += " -initrd " + args.InitrdPath
	}
	switch args.Sharedfs.Type {
	case "9pfs":
		cmdString += " -fsdev local,id=rootfs9p,security_model=none,path=" + args.Sharedfs.Path
		cmdString += " -device virtio-9p-pci,fsdev=rootfs9p,mount_tag=fs0"
	case "virtiofs":
		cmdString += " -object memory-backend-file,id=mem,size=" + qemuMem + "M,mem-path=/tmp,share=on"
		cmdString += " -numa node,memdev=mem"
		cmdString += " -chardev socket,id=char0,path=/tmp/vhostqemu"
		cmdString += " -device vhost-user-fs-pci,queue-size=1024,chardev=char0,tag=fs0"
	default:
		// Nothing to add
	}
	extraMonArgs := ukernel.MonitorCli()
	if extraMonArgs.ExtraInitrd != "" {
		cmdString += " -initrd " + extraMonArgs.ExtraInitrd
	}
	cmdString += extraMonArgs.OtherArgs

	if args.VAccelType == "vsock" {
		cmdString += " -device vhost-vsock-pci,id=vhost-vsock-pci0,guest-cid=" + fmt.Sprintf("%d", args.VSockDevID)
	}

	exArgs := strings.Split(cmdString, " ")
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

func getVirtioNetArg() string {
	devType := "virtio-net-pci"
	if runtime.GOARCH == "arm64" {
		devType = "virtio-net-device"
	}
	return "-device " + devType + ",netdev=net0"
}
