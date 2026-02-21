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
	"fmt"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const (
	CloudHypervisorVmm    VmmType = "cloud-hypervisor"
	CloudHypervisorBinary string  = "cloud-hypervisor"
)

type CloudHypervisor struct {
	binaryPath string
	binary     string
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
			exArgs = append(exArgs, "--net", fmt.Sprintf("tap=%s,mac=%s", args.Net.TapDev, args.Net.MAC))
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
