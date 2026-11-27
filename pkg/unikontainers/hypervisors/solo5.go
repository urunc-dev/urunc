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

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	HvtVmm    VmmType = "hvt"
	HvtBinary string  = "solo5-hvt"
	SptVmm    VmmType = "spt"
	SptBinary string  = "solo5-spt"
)

// HVT runs a Solo5 unikernel with the hvt tender, which boots it in a
// KVM-backed virtual machine.
type HVT struct {
	solo5
}

// UsesKVM reports that hvt needs KVM.
func (h *HVT) UsesKVM() bool {
	return true
}

// SPT runs a Solo5 unikernel with the spt tender, which runs it as a host
// process sandboxed with seccomp, so it does not need KVM.
type SPT struct {
	solo5
}

// UsesKVM reports that spt does not need KVM.
func (s *SPT) UsesKVM() bool {
	return false
}

// solo5 holds what the hvt and spt tenders share: the same cli grammar, a tap
// device for networking, no shared-fs and no control socket.
type solo5 struct {
	binaryPath string
	binary     string
}

func (s *solo5) Signal(pid int, signal unix.Signal) error {
	return signalProcess(pid, signal)
}

// Stop kills the tender process.
func (s *solo5) Stop(pid int) error {
	return killProcess(pid)
}

// UsesTUNTAP reports that the guest gets its network through a tap device.
func (s *solo5) UsesTUNTAP() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (s *solo5) SupportsSharedfs(_ string) bool {
	return false
}

// SupportsControlSocket reports that the Solo5 tenders expose no control socket.
func (s *solo5) SupportsControlSocket() bool {
	return false
}

// Path returns the path to the tender binary.
func (s *solo5) Path() string {
	return s.binaryPath
}

// Ok checks if the tender binary is available in the system's PATH.
func (s *solo5) Ok() error {
	if _, err := exec.LookPath(s.binary); err != nil {
		return ErrVMMNotInstalled
	}
	return nil
}

func (s *solo5) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	return solo5ExecCmd(s.binaryPath, args, ukernel), nil
}

// PreExec performs pre-execution setup. Neither tender needs any: solo5-hvt
// and solo5-spt apply their own seccomp filters.
func (s *solo5) PreExec(_ types.ExecArgs) error {
	return nil
}

// solo5ArgsSeparator terminates the tender options in a solo5 cli
const solo5ArgsSeparator = "--"

// solo5ExecCmd builds the command line of a solo5 tender (hvt or spt). Both
// share the cli grammar "[ OPTIONS ] [ -- ] KERNEL [ ARGS ]".
func solo5ExecCmd(binaryPath string, args types.ExecArgs, ukernel types.Unikernel) []string {
	exArgs := []string{binaryPath, "--mem=" + BytesToStringMB(args.MemSizeB)}
	if args.Net.TapDev != "" {
		netCli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
		exArgs = append(exArgs, netCli...)
	}
	bArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range bArgs {
		if blockArg.Path != "" {
			exArgs = append(exArgs, "--block:"+blockArg.ID+"="+blockArg.Path)
		}
	}
	extraMonArgs := ukernel.MonitorCli()
	exArgs = append(exArgs, extraMonArgs.OtherArgs...)
	exArgs = append(exArgs, solo5ArgsSeparator, args.UnikernelPath)
	if args.Command != "" {
		exArgs = append(exArgs, args.Command)
	}
	return exArgs
}
