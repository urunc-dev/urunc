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
	"strconv"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	HviVmm    VmmType = "hvi"
	HviBinary string  = "hvi"
)

type HVI struct {
	binaryPath string
	binary     string
}

func (h *HVI) Signal(pid int, signal unix.Signal) error {
	return signalProcess(pid, signal)
}

func (h *HVI) Stop(pid int) error {
	return killProcess(pid)
}

func (h *HVI) Ok() error {
	return nil
}

// UsesKVM returns true as hvi is a KVM-based VMM on Linux
func (h *HVI) UsesKVM() bool {
	return true
}

// SupportsControlSocket reports that hvi exposes no control/API socket.
func (h *HVI) SupportsControlSocket() bool {
	return false
}

// SupportsSharedfs returns true as hvi supports vhost-user virtiofs on Linux
func (h *HVI) SupportsSharedfs(fsType string) bool {
	switch fsType {
	case "virtio":
		return true
	default:
		return false
	}
}

func (h *HVI) Path() string {
	return h.binaryPath
}

// BuildExecCmd builds and validates the hvi command arguments without executing.
func (h *HVI) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	if args.UnikernelPath == "" {
		return nil, errors.New("kernel path is required for hvi")
	}

	// hvi requires the "boot" subcommand
	exArgs := []string{h.binaryPath, "boot", "--kernel", args.UnikernelPath}

	// Memory configuration in MiB
	if args.MemSizeB > 0 {
		memMiB := bytesToMiB(args.MemSizeB)
		if memMiB == 0 {
			memMiB = DefaultMemory
		}
		exArgs = append(exArgs, "--mem-mib", strconv.FormatUint(memMiB, 10))
	}

	// vCPU configuration
	if args.VCPUs > 0 {
		exArgs = append(exArgs, "--cpus", strconv.FormatUint(uint64(args.VCPUs), 10))
	}

	// Network configuration (separate --net-tap and --net-mac flags)
	if args.Net.TapDev != "" {
		exArgs = append(exArgs, "--net-tap", args.Net.TapDev)
	}
	if args.Net.MAC != "" {
		exArgs = append(exArgs, "--net-mac", args.Net.MAC)
	}

	// Block device configuration: --disk path=<path>[,serial=<id>]
	blockArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range blockArgs {
		if len(blockArg.ExactArgs) > 0 {
			exArgs = append(exArgs, blockArg.ExactArgs...)
		} else if blockArg.Path != "" {
			diskArg := "path=" + blockArg.Path
			if blockArg.ID != "" {
				diskArg += ",serial=" + blockArg.ID
			}
			exArgs = append(exArgs, "--disk", diskArg)
		}
	}

	// Initramfs configuration (hvi supports a single --initramfs flag)
	extraMonArgs := ukernel.MonitorCli()
	initrdPath := args.InitrdPath
	if initrdPath == "" {
		initrdPath = extraMonArgs.ExtraInitrd
	}
	if initrdPath != "" {
		exArgs = append(exArgs, "--initramfs", initrdPath)
	}

	// Shared filesystem configuration (virtiofs over vhost-user)
	switch args.Sharedfs.Type {
	case "virtiofs":
		exArgs = append(exArgs, "--fs", "tag=fs0,socket=/tmp/vhostqemu")
	default:
		// No shared filesystem
	}

	// Seccomp sandbox configuration (--no-sandbox when seccomp is disabled)
	if !args.Seccomp {
		exArgs = append(exArgs, "--no-sandbox")
	}

	if len(extraMonArgs.OtherArgs) > 0 {
		exArgs = append(exArgs, extraMonArgs.OtherArgs...)
	}

	// Command line arguments for the guest kernel
	if args.Command != "" {
		exArgs = append(exArgs, "--cmdline", args.Command)
	}

	vmmLog.WithField("hvi command", exArgs).Debug("Ready to execve hvi")

	return exArgs, nil
}

// PreExec performs pre-execution setup. hvi has no special pre-exec requirements.
func (h *HVI) PreExec(_ types.ExecArgs) error {
	return nil
}
