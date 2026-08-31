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

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	HyperlightVmm    VmmType = "hyperlight-unikraft"
	HyperlightBinary string  = "hyperlight-unikraft"
)

type Hyperlight struct {
	binaryPath string
	binary     string
}

// Stop kills the hyperlight process
func (h *Hyperlight) Stop(pid int) error {
	return killProcess(pid)
}

// UsesKVM returns a bool value depending on if the monitor uses KVM
func (h *Hyperlight) UsesKVM() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (h *Hyperlight) SupportsSharedfs(_ string) bool {
	return false
}

// SupportsControlSocket reports that Hyperlight exposes no control socket.
func (h *Hyperlight) SupportsControlSocket() bool {
	return false
}

func (h *Hyperlight) SupportsGuestShutdown() bool {
	return false
}

func (h *Hyperlight) RequestGuestShutdown(_ string) error {
	return fmt.Errorf("guest shutdown not supported for hyperlight")
}

// Path returns the path to the hyperlight binary.
func (h *Hyperlight) Path() string {
	return h.binaryPath
}

// Ok checks if the hyperlight-unikraft binary is available.
// Binary availability is already verified by getVMMPath via exec.LookPath.
func (h *Hyperlight) Ok() error {
	return nil
}

func (h *Hyperlight) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

// BuildExecCmd constructs the hyperlight-unikraft command line.
func (h *Hyperlight) BuildExecCmd(args types.ExecArgs, _ types.Unikernel) ([]string, error) {
	cmdArgs := []string{h.binaryPath, args.UnikernelPath}
	if args.InitrdPath != "" {
		cmdArgs = append(cmdArgs, "--initrd", args.InitrdPath)
	}
	if args.MemSizeB > 0 {
		cmdArgs = append(cmdArgs, "--memory", fmt.Sprintf("%d", args.MemSizeB))
	}
	return cmdArgs, nil
}

// PreExec performs pre-execution setup. Hyperlight has no special pre-exec requirements.
func (h *Hyperlight) PreExec(_ types.ExecArgs) error {
	return nil
}
