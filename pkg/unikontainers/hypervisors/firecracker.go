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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	FirecrackerVmm    VmmType = "firecracker"
	FirecrackerBinary string  = "firecracker"
	FCJsonFilename    string  = "fc.json"
)

type Firecracker struct {
	binaryPath string
	binary     string
}

type FirecrackerBootSource struct {
	ImagePath  string `json:"kernel_image_path"`
	BootArgs   string `json:"boot_args"`
	InitrdPath string `json:"initrd_path,omitempty"`
}

type FirecrackerMachine struct {
	VcpuCount       uint   `json:"vcpu_count"`
	MemSizeMiB      uint64 `json:"mem_size_mib"`
	Smt             bool   `json:"smt"`
	TrackDirtyPages bool   `json:"track_dirty_pages"`
}

type FirecrackerDrive struct {
	DriveID   string `json:"drive_id"`
	IsRO      bool   `json:"is_read_only"`
	IsRootDev bool   `json:"is_root_device"`
	HostPath  string `json:"path_on_host"`
}

type FirecrackerNet struct {
	IfaceID  string `json:"iface_id"`
	GuestMAC string `json:"guest_mac,omitempty"`
	HostIF   string `json:"host_dev_name"`
}

type FirecrackerVSockDev struct {
	GuestCID int    `json:"guest_cid"`
	UDSPath  string `json:"uds_path"`
	VSockID  string `json:"vsock_id"`
}

type FirecrackerConfig struct {
	Source  FirecrackerBootSource `json:"boot-source"`
	Machine FirecrackerMachine    `json:"machine-config"`
	Drives  []FirecrackerDrive    `json:"drives"`
	NetIfs  []FirecrackerNet      `json:"network-interfaces,omitempty"`
	VSock   FirecrackerVSockDev   `json:"vsock,omitempty"`
}

func (fc *Firecracker) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

func (fc *Firecracker) Stop(pid int) error {
	return killProcess(pid)
}

func (fc *Firecracker) Ok() error {
	return nil
}

func (fc *Firecracker) UsesKVM() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (fc *Firecracker) SupportsSharedfs(_ string) bool {
	return false
}

func (fc *Firecracker) Path() string {
	return fc.binaryPath
}

func (fc *Firecracker) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	// FIXME: Note for getting unikernel specific options.
	// Due to the way FC operates, we have not encountered any guest specific
	// options yet. However, we need to revisit how we can use guest specific
	// options in FC, since the string return value of the Monitor related
	// functions in the unikernel interface do not integrate well with FC's
	// json configuration.
	apiSockPath := ResolveSocketPath(args)
	cmdString := fc.Path() + " --api-sock " + apiSockPath
	JSONConfigFile := filepath.Join("/tmp/", FCJsonFilename)
	if args.BootMode == "config-file" {
		cmdString += " --config-file " + JSONConfigFile
	}
	if !args.Seccomp {
		cmdString += " --no-seccomp"
	}

	FCConfig := buildFirecrackerConfig(args, ukernel)
	FCConfigJSON, err := json.Marshal(FCConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Firecracker config: %w", err)
	}
	if err = os.WriteFile(JSONConfigFile, FCConfigJSON, 0o644); err != nil { //nolint: gosec
		return nil, fmt.Errorf("failed to save Firecracker json config: %w", err)
	}
	vmmLog.WithField("Json", string(FCConfigJSON)).Debug("Firecracker json config")

	exArgs := strings.Split(cmdString, " ")
	return exArgs, nil
}

func (fc *Firecracker) PreExec(_ types.ExecArgs) error {
	return nil
}

// buildFirecrackerConfig builds the microVM configuration. Both boot modes
// use it, so they always configure the guest the same way.
func buildFirecrackerConfig(args types.ExecArgs, ukernel types.Unikernel) *FirecrackerConfig {
	// VM config for Firecracker
	fcMem := DefaultMemory
	if args.MemSizeB != 0 {
		fcMem = bytesToMiB(args.MemSizeB)
		// Check if memory is too small
		if fcMem == 0 {
			fcMem = DefaultMemory
		}
	}
	// NOTE: Firecracker supports only one initrd.
	// Therefore, we depend on the guest/unikernel implementation
	// to properly handle that case and concatenate the initrd
	// files if there are more than one. Hence, always give priority
	// to the initrd taken from args.
	extraMonArgs := ukernel.MonitorCli()
	initrdPath := args.InitrdPath
	if initrdPath == "" {
		initrdPath = extraMonArgs.ExtraInitrd
	}
	FCMachine := FirecrackerMachine{
		VcpuCount:       args.VCPUs,
		MemSizeMiB:      fcMem,
		Smt:             false,
		TrackDirtyPages: false,
	}

	// Net config for Firecracker
	FCNet := make([]FirecrackerNet, 0)
	if args.Net.TapDev != "" {
		AnIF := FirecrackerNet{
			IfaceID:  "net1",
			GuestMAC: args.Net.MAC,
			HostIF:   args.Net.TapDev,
		}
		FCNet = append(FCNet, AnIF)
	}

	// Block config for Firecracker
	// TODO: Add support for block devices in FIrecracker
	FCDrives := make([]FirecrackerDrive, 0)

	bArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range bArgs {
		aBlock := FirecrackerDrive{
			DriveID:   blockArg.ID,
			IsRO:      false,
			IsRootDev: false,
			HostPath:  blockArg.Path,
		}
		if blockArg.ID == "rootfs" {
			aBlock.IsRootDev = true
		}
		FCDrives = append(FCDrives, aBlock)
	}
	FCSource := FirecrackerBootSource{
		ImagePath:  args.UnikernelPath,
		BootArgs:   args.Command,
		InitrdPath: initrdPath,
	}

	var FCVSockDev FirecrackerVSockDev
	if args.VAccelType == "vsock" {
		FCVSockDev = FirecrackerVSockDev{
			GuestCID: args.VSockDevID,
			UDSPath:  args.VSockDevPath + "/vaccel.sock",
			VSockID:  "root",
		}
	}

	return &FirecrackerConfig{
		Source:  FCSource,
		Machine: FCMachine,
		Drives:  FCDrives,
		NetIfs:  FCNet,
		VSock:   FCVSockDev,
	}
}

// FirecrackerSession is a Firecracker child process that urunc configures
// over its API socket, one resource at a time.
type FirecrackerSession struct {
	cmd    *exec.Cmd
	client *firecrackerClient
}

// SpawnSocketVMM starts Firecracker with only its API socket. It must run
// after changeRoot, so the socket stays inside the monitor rootfs.
func (fc *Firecracker) SpawnSocketVMM(args types.ExecArgs, uid, gid uint32) (*FirecrackerSession, error) {
	socketPath := ResolveSocketPath(args)
	// Firecracker binds this path, and a leftover file makes the bind fail.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove stale socket %q: %w", socketPath, err)
	}
	execCmd := []string{fc.Path(), "--api-sock", socketPath}
	if !args.Seccomp {
		execCmd = append(execCmd, "--no-seccomp")
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
	vmmLog.WithField("command", execCmd).Debug("starting Firecracker as a supervised child")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start firecracker: %w", err)
	}

	client := newFirecrackerClient(socketPath)
	if err := client.connect(5 * time.Second); err != nil {
		s := &FirecrackerSession{cmd: cmd, client: client}
		s.Kill()
		return nil, fmt.Errorf("firecracker socket never became ready: %w", err)
	}
	return &FirecrackerSession{cmd: cmd, client: client}, nil
}

// ConfigureMachine sends the vCPU and memory configuration.
func (s *FirecrackerSession) ConfigureMachine(ctx context.Context, args types.ExecArgs) error {
	fcMem := DefaultMemory
	if args.MemSizeB != 0 {
		fcMem = bytesToMiB(args.MemSizeB)
		if fcMem == 0 {
			fcMem = DefaultMemory
		}
	}
	machine := FirecrackerMachine{
		VcpuCount:       args.VCPUs,
		MemSizeMiB:      fcMem,
		Smt:             false,
		TrackDirtyPages: false,
	}
	vmmLog.Debug("staged boot: sending machine-config")
	return s.client.putMachineConfig(ctx, machine)
}

// ConfigureNetwork attaches the container's network interface. Firecracker
// opens the tap device here, so the tap must exist first. It does nothing
// when there is no tap.
func (s *FirecrackerSession) ConfigureNetwork(ctx context.Context, net types.NetDevParams) error {
	if net.TapDev == "" {
		return nil
	}
	iface := FirecrackerNet{
		IfaceID:  "net1",
		GuestMAC: net.MAC,
		HostIF:   net.TapDev,
	}
	vmmLog.Debug("staged boot: sending network-interfaces")
	return s.client.putNetworkIface(ctx, iface)
}

// ConfigureGuest sends the block devices, the boot source and the vsock
// device. It must run after unikernel.Init, which produces the block device
// list and the boot arguments.
func (s *FirecrackerSession) ConfigureGuest(ctx context.Context, args types.ExecArgs, ukernel types.Unikernel) error {
	cfg := buildFirecrackerConfig(args, ukernel)

	vmmLog.Debug("staged boot: sending drives, boot-source and vsock")
	if err := s.client.putDrives(ctx, cfg.Drives); err != nil {
		return err
	}
	if err := s.client.putBootSource(ctx, cfg.Source); err != nil {
		return err
	}
	return s.client.putVSock(ctx, cfg.VSock)
}

// StartGuest powers on the configured microVM.
func (s *FirecrackerSession) StartGuest(ctx context.Context) error {
	vmmLog.Debug("staged boot: sending InstanceStart")
	return s.client.startGuest(ctx)
}

// Kill terminates the child process and reaps it.
func (s *FirecrackerSession) Kill() {
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
}

// Supervise forwards signals to Firecracker and exits with its exit code once
// it exits. It does not return on success.
func (s *FirecrackerSession) Supervise() error {
	// Forward the signals that stop a container. SIGKILL cannot be caught,
	// so this process dies at once and leaves Firecracker running.
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
			vmmLog.WithError(waitErr).Error("firecracker exited with an unexpected error")
			exitCode = 1
		}
	}
	os.Exit(exitCode)
	return nil // unreachable
}
