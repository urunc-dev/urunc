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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	CloudHypervisorVmm    VmmType = "cloud-hypervisor"
	CloudHypervisorBinary string  = "cloud-hypervisor"
)

type CloudHypervisor struct {
	binaryPath string
	binary     string
}

func (ch *CloudHypervisor) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

func (ch *CloudHypervisor) Stop(pid int) error {
	return killProcess(pid)
}

func (ch *CloudHypervisor) Ok() error {
	return nil
}

// UsesKVM returns true as Cloud Hypervisor is a KVM-based VMM
func (ch *CloudHypervisor) UsesKVM() bool {
	return true
}

// SupportsSharedfs returns true as Cloud Hypervisor supports virtiofs
func (ch *CloudHypervisor) SupportsSharedfs(fsType string) bool {
	switch fsType {
	case "virtio":
		return true
	default:
		return false
	}
}

func (ch *CloudHypervisor) Path() string {
	return ch.binaryPath
}

// BuildExecCmd builds and validates the Cloud Hypervisor command arguments without executing.
func (ch *CloudHypervisor) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	chMem := BytesToStringMB(args.MemSizeB)

	// Start building the command
	exArgs := []string{ch.binaryPath}

	exArgs = append(exArgs, "--api-socket", "path="+ResolveSocketPath(args))

	// Memory configuration
	if args.Sharedfs.Type == "virtiofs" {
		exArgs = append(exArgs, "--memory", fmt.Sprintf("size=%sM,shared=on", chMem))
	} else {
		exArgs = append(exArgs, "--memory", fmt.Sprintf("size=%sM", chMem))
	}

	// CPU configuration
	if args.VCPUs > 0 {
		exArgs = append(exArgs, "--cpus", fmt.Sprintf("boot=%d", args.VCPUs))
	}

	// Kernel path
	exArgs = append(exArgs, "--kernel", args.UnikernelPath)

	// Console configuration - disable graphical output
	exArgs = append(exArgs, "--console", "off", "--serial", "tty")

	// Seccomp configuration
	if args.Seccomp {
		exArgs = append(exArgs, "--seccomp", "true")
	} else {
		exArgs = append(exArgs, "--seccomp", "false")
	}

	// Network configuration
	if args.Net.TapDev != "" {
		netCli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
		if netCli == "" {
			// Default network configuration for Cloud Hypervisor
			exArgs = append(exArgs, "--net", fmt.Sprintf("tap=%s,mac=%s,mtu=%d", args.Net.TapDev, args.Net.MAC, args.Net.MTU))
		} else {
			exArgs = append(exArgs, strings.Split(strings.TrimSpace(netCli), " ")...)
		}
	}

	// Block device configuration
	blockArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range blockArgs {
		if blockArg.ExactArgs != "" {
			exArgs = append(exArgs, strings.Split(strings.TrimSpace(blockArg.ExactArgs), " ")...)
		} else if blockArg.Path != "" {
			diskArg := fmt.Sprintf("path=%s", blockArg.Path)
			if blockArg.ID != "" {
				diskArg += fmt.Sprintf(",id=%s", blockArg.ID)
			}
			exArgs = append(exArgs, "--disk", diskArg)
		}
	}

	// Initrd configuration
	if args.InitrdPath != "" {
		exArgs = append(exArgs, "--initramfs", args.InitrdPath)
	}

	// Check for extra initrd from unikernel monitor args
	extraMonArgs := ukernel.MonitorCli()
	if extraMonArgs.ExtraInitrd != "" {
		exArgs = append(exArgs, "--initramfs", extraMonArgs.ExtraInitrd)
	}

	switch args.Sharedfs.Type {
	case "virtiofs":
		exArgs = append(exArgs, "--fs", "tag=fs0,socket=/tmp/vhostqemu")
	default:
		// No shared filesystem
	}

	if args.VAccelType == "vsock" {
		exArgs = append(exArgs, "--vsock", fmt.Sprintf("cid=%d,socket=%s/vaccel.sock",
			args.VSockDevID, args.VSockDevPath))
	}

	if extraMonArgs.OtherArgs != "" {
		exArgs = append(exArgs, strings.Split(strings.TrimSpace(extraMonArgs.OtherArgs), " ")...)
	}

	// Add the command line arguments for the kernel
	exArgs = append(exArgs, "--cmdline", args.Command)

	vmmLog.WithField("cloud-hypervisor command", exArgs).Debug("Ready to execve cloud-hypervisor")

	return exArgs, nil
}

// PreExec performs pre-execution setup. Cloud Hypervisor has no special pre-exec requirements.
func (ch *CloudHypervisor) PreExec(_ types.ExecArgs) error {
	return nil
}

// These types mirror the part of Cloud Hypervisor's REST VmConfig that the
// command-line boot already uses. Checked against cloud-hypervisor v53.

type CHPayload struct {
	Kernel    string `json:"kernel,omitempty"`
	Cmdline   string `json:"cmdline,omitempty"`
	Initramfs string `json:"initramfs,omitempty"`
}

type CHMemory struct {
	Size   uint64 `json:"size"`
	Shared bool   `json:"shared,omitempty"`
}

type CHCpus struct {
	BootVcpus uint `json:"boot_vcpus"`
	MaxVcpus  uint `json:"max_vcpus"`
}

type CHNet struct {
	Tap string `json:"tap,omitempty"`
	Mac string `json:"mac,omitempty"`
	Mtu int    `json:"mtu,omitempty"`
}

type CHDisk struct {
	Path string `json:"path"`
	ID   string `json:"id,omitempty"`
}

type CHFs struct {
	Tag    string `json:"tag"`
	Socket string `json:"socket"`
}

type CHVsock struct {
	Cid    int    `json:"cid"`
	Socket string `json:"socket"`
}

type CHConsole struct {
	Mode string `json:"mode"`
}

type CHVMConfig struct {
	Payload CHPayload  `json:"payload"`
	Memory  *CHMemory  `json:"memory,omitempty"`
	Cpus    *CHCpus    `json:"cpus,omitempty"`
	Net     []CHNet    `json:"net,omitempty"`
	Disks   []CHDisk   `json:"disks,omitempty"`
	Fs      []CHFs     `json:"fs,omitempty"`
	Vsock   *CHVsock   `json:"vsock,omitempty"`
	Serial  *CHConsole `json:"serial,omitempty"`
	Console *CHConsole `json:"console,omitempty"`
}

// buildCHVMConfig maps the command-line data into the REST VmConfig. Raw CLI
// argument strings have no JSON form, so they return an error.
func buildCHVMConfig(args types.ExecArgs, ukernel types.Unikernel) (*CHVMConfig, error) {
	mem := args.MemSizeB
	if mem < 1<<20 {
		mem = DefaultMemory << 20
	}
	cfg := &CHVMConfig{
		Payload: CHPayload{Kernel: args.UnikernelPath, Cmdline: args.Command},
		Memory:  &CHMemory{Size: mem, Shared: args.Sharedfs.Type == "virtiofs"},
		Serial:  &CHConsole{Mode: "Tty"},
		Console: &CHConsole{Mode: "Off"},
	}
	if args.VCPUs > 0 {
		cfg.Cpus = &CHCpus{BootVcpus: args.VCPUs, MaxVcpus: args.VCPUs}
	}

	extraMonArgs := ukernel.MonitorCli()
	if extraMonArgs.OtherArgs != "" {
		return nil, fmt.Errorf("boot_mode=api does not support unikernel-specific monitor arguments (%q)", extraMonArgs.OtherArgs)
	}
	initrdPath := args.InitrdPath
	if initrdPath == "" {
		initrdPath = extraMonArgs.ExtraInitrd
	}
	cfg.Payload.Initramfs = initrdPath

	if args.Net.TapDev != "" {
		if netCli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC); netCli != "" {
			return nil, fmt.Errorf("boot_mode=api does not support unikernel-specific network arguments (%q)", netCli)
		}
		cfg.Net = append(cfg.Net, CHNet{Tap: args.Net.TapDev, Mac: args.Net.MAC, Mtu: args.Net.MTU})
	}

	for _, blockArg := range ukernel.MonitorBlockCli() {
		if blockArg.ExactArgs != "" {
			return nil, fmt.Errorf("boot_mode=api does not support unikernel-specific block arguments (%q)", blockArg.ExactArgs)
		}
		if blockArg.Path != "" {
			cfg.Disks = append(cfg.Disks, CHDisk{Path: blockArg.Path, ID: blockArg.ID})
		}
	}

	if args.Sharedfs.Type == "virtiofs" {
		cfg.Fs = append(cfg.Fs, CHFs{Tag: "fs0", Socket: "/tmp/vhostqemu"})
	}
	if args.VAccelType == "vsock" {
		cfg.Vsock = &CHVsock{Cid: args.VSockDevID, Socket: args.VSockDevPath + "/vaccel.sock"}
	}
	return cfg, nil
}

// CHSession is a Cloud Hypervisor child process that urunc drives over its
// REST API socket.
type CHSession struct {
	cmd    *exec.Cmd
	client *chAPIClient
}

// SpawnSocketVMM starts Cloud Hypervisor with only its API socket. It must
// run after changeRoot, so the socket stays inside the monitor rootfs.
func (ch *CloudHypervisor) SpawnSocketVMM(args types.ExecArgs, uid, gid uint32) (*CHSession, error) {
	socketPath := ResolveSocketPath(args)
	// Cloud Hypervisor binds this path, and a leftover file makes the bind fail.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove stale socket %q: %w", socketPath, err)
	}
	execCmd := []string{ch.binaryPath, "--api-socket", "path=" + socketPath}
	if args.Seccomp {
		execCmd = append(execCmd, "--seccomp", "true")
	} else {
		execCmd = append(execCmd, "--seccomp", "false")
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
	vmmLog.WithField("command", execCmd).Debug("starting cloud-hypervisor as a supervised child")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start cloud-hypervisor: %w", err)
	}

	client := newCHAPIClient(socketPath)
	if err := client.connect(5 * time.Second); err != nil {
		s := &CHSession{cmd: cmd, client: client}
		s.Kill()
		return nil, fmt.Errorf("cloud-hypervisor API socket never became ready: %w", err)
	}
	return &CHSession{cmd: cmd, client: client}, nil
}

// ConfigureVM sends the complete VM configuration in one request.
func (s *CHSession) ConfigureVM(ctx context.Context, args types.ExecArgs, ukernel types.Unikernel) error {
	cfg, err := buildCHVMConfig(args, ukernel)
	if err != nil {
		return err
	}
	vmmLog.Debug("api boot: sending vm.create")
	return s.client.createVM(ctx, cfg)
}

func (s *CHSession) BootVM(ctx context.Context) error {
	vmmLog.Debug("api boot: sending vm.boot")
	return s.client.bootVM(ctx)
}

// Kill terminates the child process and reaps it.
func (s *CHSession) Kill() {
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
}

// Supervise forwards signals to Cloud Hypervisor and exits with its exit code
// once it exits. It does not return on success.
func (s *CHSession) Supervise() error {
	// Forward the signals that stop a container. SIGKILL cannot be caught, so
	// this process dies at once and leaves Cloud Hypervisor running.
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
			vmmLog.WithError(waitErr).Error("cloud-hypervisor exited with an unexpected error")
			exitCode = 1
		}
	}
	os.Exit(exitCode)
	return nil // unreachable
}
