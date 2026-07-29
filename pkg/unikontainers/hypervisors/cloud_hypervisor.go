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
	"strconv"

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

	// Memory configuration
	if args.Sharedfs.Type == "virtiofs" {
		memArg := "size=" + chMem + "M,shared=on"
		exArgs = append(exArgs, "--memory", memArg)
	} else {
		memArg := "size=" + chMem + "M"
		exArgs = append(exArgs, "--memory", memArg)
	}

	// CPU configuration
	if args.VCPUs > 0 {
		cpuArg := "boot=" + strconv.FormatUint(uint64(args.VCPUs), 10)
		exArgs = append(exArgs, "--cpus", cpuArg)
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
		if len(netCli) == 0 {
			// Default network configuration for Cloud Hypervisor
			netArg := fmt.Sprintf("tap=%s,mac=%s,mtu=%d", args.Net.TapDev, args.Net.MAC, args.Net.MTU)
			exArgs = append(exArgs, "--net", netArg)
		} else {
			exArgs = append(exArgs, netCli...)
		}
	}

	// Block device configuration
	blockArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range blockArgs {
		if len(blockArg.ExactArgs) > 0 {
			exArgs = append(exArgs, blockArg.ExactArgs...)
		} else if blockArg.Path != "" {
			diskArg := "path=" + blockArg.Path
			if blockArg.ID != "" {
				diskArg += ",id=" + blockArg.ID
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
		vsockArg := fmt.Sprintf("cid=%d,socket=%s/vaccel.sock", args.VSockDevID, args.VSockDevPath)
		exArgs = append(exArgs, "--vsock", vsockArg)
	}

	if len(extraMonArgs.OtherArgs) > 0 {
		exArgs = append(exArgs, extraMonArgs.OtherArgs...)
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
