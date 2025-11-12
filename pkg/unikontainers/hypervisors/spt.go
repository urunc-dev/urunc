// Copyright (c) 2023-2025, Nubificus LTD
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
	"syscall"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const (
	SptVmm    VmmType = "spt"
	SptBinary string  = "solo5-spt"
)

type SPT struct {
	binaryPath string
	binary     string
}

// Stop kills the spt process
func (s *SPT) Stop(pid int) error {
	return killProcess(pid)
}

// UsesKVM returns a bool value depending on if the monitor uses KVM
func (s *SPT) UsesKVM() bool {
	return false
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (s *SPT) SupportsSharedfs(_ string) bool {
	return false
}

// Path returns the path to the spt binary.
func (s *SPT) Path() string {
	return s.binaryPath
}

// Ok checks if the spt binary is available in the system's PATH.
func (s *SPT) Ok() error {
	if _, err := exec.LookPath(SptBinary); err != nil {
		return ErrVMMNotInstalled
	}
	return nil
}

func (s *SPT) Execve(args types.ExecArgs, ukernel types.Unikernel) error {
	sptMem := BytesToStringMB(args.MemSizeB)
	cmdString := s.binaryPath + " --mem=" + sptMem
	if args.Net.TapDev != "" {
		cmdString += " "
		cmdString += ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
	}
	bArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range bArgs {
		cmdString = appendNonEmpty(cmdString, " --block:"+blockArg.ID+"=",
			blockArg.Path)
	}
	extraMonArgs := ukernel.MonitorCli()
	cmdString = appendNonEmpty(cmdString, " ", extraMonArgs.OtherArgs)
	cmdString += " " + args.UnikernelPath + " " + args.Command
	cmdArgs := strings.Split(cmdString, " ")
	vmmLog.WithField("spt command", cmdString).Debug("Ready to execve spt")
	return syscall.Exec(s.binaryPath, cmdArgs, args.Environment) //nolint: gosec
}
