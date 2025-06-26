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

	"github.com/nubificus/urunc/pkg/unikontainers/unikernels"
)

const (
	SptVmm    VmmType = "spt"
	SptBinary string  = "solo5-spt"
)

type SPT struct {
	binaryPath string
	binary     string
}

// Stop is an empty function to satisfy VMM interface compatibility requirements.
// It does not perform any actions and always returns nil.
func (s *SPT) Stop(_ string) error {
	return nil
}

// UsesKVM returns a bool value depending on if the monitor uses KVM
func (s *SPT) UsesKVM() bool {
	return false
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (s *SPT) SupportsSharedfs() bool {
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

func (s *SPT) Execve(args ExecArgs, ukernel unikernels.Unikernel) error {
	sptString := string(SptVmm)
	sptMem := bytesToStringMB(args.MemSizeB)
	cmdString := s.binaryPath + " --mem=" + sptMem
	cmdString = appendNonEmpty(cmdString, " "+ukernel.MonitorNetCli(sptString), args.TapDevice)
	cmdString = appendNonEmpty(cmdString, " "+ukernel.MonitorBlockCli(sptString), args.BlockDevice)
	cmdString = appendNonEmpty(cmdString, " ", ukernel.MonitorCli(sptString))
	cmdString += " " + args.UnikernelPath + " " + args.Command
	cmdArgs := strings.Split(cmdString, " ")
	vmmLog.WithField("spt command", cmdString).Debug("Ready to execve spt")
	return syscall.Exec(s.binaryPath, cmdArgs, args.Environment) //nolint: gosec
}
