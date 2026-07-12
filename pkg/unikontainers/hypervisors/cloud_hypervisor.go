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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	// The API socket stays enabled so a running microVM can later be
	// paused/resumed and snapshotted (checkpoint/restore).
	exArgs := []string{ch.binaryPath, "--api-socket", InNsAPISockPath}

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

// CHSnapshotConfigFile is the name of the VM configuration file inside a
// Cloud Hypervisor snapshot directory. Cloud Hypervisor additionally writes
// state.json and one or more memory-ranges files in the same directory.
const CHSnapshotConfigFile string = "config.json"

// SupportsSnapshot returns true as Cloud Hypervisor supports snapshot/restore
// through its HTTP API.
func (ch *CloudHypervisor) SupportsSnapshot() bool {
	return true
}

// PauseVM pauses the vCPUs of a running Cloud Hypervisor microVM.
func (ch *CloudHypervisor) PauseVM(sockPath string) error {
	client := newVMMAPIClient(sockPath)
	return client.request("PUT", "/api/v1/vm.pause", nil)
}

// ResumeVM resumes the vCPUs of a paused Cloud Hypervisor microVM.
func (ch *CloudHypervisor) ResumeVM(sockPath string) error {
	client := newVMMAPIClient(sockPath)
	return client.request("PUT", "/api/v1/vm.resume", nil)
}

// SnapshotVM writes a full snapshot (config.json, state.json and memory
// ranges) of a paused microVM into inNsDir, which Cloud Hypervisor resolves
// inside its own mount namespace.
func (ch *CloudHypervisor) SnapshotVM(sockPath string, inNsDir string) error {
	client := newVMMAPIClient(sockPath)
	body := map[string]string{
		"destination_url": "file://" + inNsDir,
	}
	return client.request("PUT", "/api/v1/vm.snapshot", body)
}

// BuildRestoreCmd builds the argv to launch a fresh Cloud Hypervisor process
// that restores the VM from the snapshot staged in inNsDir at process start.
// The restored VM stays paused until FinishRestore resumes it.
func (ch *CloudHypervisor) BuildRestoreCmd(args types.ExecArgs, inNsDir string) ([]string, error) {
	exArgs := []string{
		ch.binaryPath,
		"--api-socket", InNsAPISockPath,
		"--restore", "source_url=file://" + inNsDir,
	}
	if args.Seccomp {
		exArgs = append(exArgs, "--seccomp", "true")
	} else {
		exArgs = append(exArgs, "--seccomp", "false")
	}
	return exArgs, nil
}

// PrepareRestore rewrites the tap device name in the staged snapshot's
// config.json so the restored VM attaches to the freshly-created host tap
// device. The guest-visible side of the device (MAC, IP) is frozen in the
// snapshot and must not change.
func (ch *CloudHypervisor) PrepareRestore(hostDir string, net NetOverride) error {
	if net.TapDev == "" {
		return nil
	}

	configPath := filepath.Join(hostDir, CHSnapshotConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read snapshot VM config %s: %w", configPath, err)
	}

	// Parse the VM config generically so we do not have to track the full
	// Cloud Hypervisor VmConfig schema; only the tap name is rewritten.
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse snapshot VM config %s: %w", configPath, err)
	}

	netDevs, ok := config["net"].([]any)
	if !ok || len(netDevs) == 0 {
		// The snapshotted VM had no network device; nothing to rewrite.
		return nil
	}
	for _, dev := range netDevs {
		netDev, ok := dev.(map[string]any)
		if !ok {
			continue
		}
		netDev["tap"] = net.TapDev
	}

	patched, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot VM config: %w", err)
	}
	return os.WriteFile(configPath, patched, 0o644) //nolint: gosec
}

// FinishRestore resumes the restored microVM. The state load itself already
// happened at process start through the --restore flag.
func (ch *CloudHypervisor) FinishRestore(sockPath string, _ string, _ NetOverride) error {
	return ch.ResumeVM(sockPath)
}
