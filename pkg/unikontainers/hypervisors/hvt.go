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
	"os/exec"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	HvtVmm    VmmType = "hvt"
	HvtBinary string  = "solo5-hvt"
)

type HVT struct {
	binaryPath string
	binary     string
}

func (h *HVT) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

// Stop kills the hvt process
func (h *HVT) Stop(pid int) error {
	return killProcess(pid)
}

// UsesKVM returns a bool value depending on if the monitor uses KVM
func (h *HVT) UsesKVM() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (h *HVT) SupportsSharedfs(_ string) bool {
	return false
}

// Path returns the path to the hvt binary.
func (h *HVT) Path() string {
	return h.binaryPath
}

// Ok checks if the hvt binary is available in the system's PATH.
func (h *HVT) Ok() error {
	if _, err := exec.LookPath(HvtBinary); err != nil {
		return ErrVMMNotInstalled
	}
	return nil
}

func (h *HVT) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	hvtMem := BytesToStringMB(args.MemSizeB)
	cmdString := h.binaryPath + " --mem=" + hvtMem
	if args.Net.TapDev != "" {
		cmdString += " "
		cmdString += ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
	}
	extraMonArgs := ukernel.MonitorCli()
	bArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range bArgs {
		cmdString = appendNonEmpty(cmdString, " --block:"+blockArg.ID+"=",
			blockArg.Path)
	}
	cmdString = appendNonEmpty(cmdString, " ", extraMonArgs.OtherArgs)
	cmdString += " " + args.UnikernelPath + " " + args.Command
	cmdArgs := strings.Split(cmdString, " ")
	return cmdArgs, nil
}

// PreExec performs pre-execution setup for HVT.
// Since Solo5 0.11.0, solo5-hvt applies its own seccomp filter internally.
func (h *HVT) PreExec(_ types.ExecArgs) error {
	return nil
}
