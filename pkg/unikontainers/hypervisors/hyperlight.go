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
	HyperlightVmm    VmmType = "hyperlight-unikraft"
	HyperlightBinary string  = "hluk"
)

var ErrHyperlightNoInitrd = errors.New("hyperlight-unikraft requires an initrd")

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

// Path returns the path to the hluk binary.
func (h *Hyperlight) Path() string {
	return h.binaryPath
}

// Ok checks if the hluk binary is available.
// Binary availability is already verified by getVMMPath via exec.LookPath.
func (h *Hyperlight) Ok() error {
	return nil
}

func (h *Hyperlight) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

// BuildExecCmd constructs the hluk command line: the kernel of the image,
// the initrd (the rootfs CPIO) sized by its scratch memory, plus whatever the
// unikernel asks for through MonitorCli, such as the guest command. hluk
// embeds a Unikraft kernel of its own, so the unikernel binary is passed as
// --kernel, which boots it in place of the embedded one, and an image
// without one is left to the embedded kernel. An image that ships a saved
// snapshot is resumed instead of booted.
func (h *Hyperlight) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	extraMonArgs := ukernel.MonitorCli()
	// A snapshot is a saved guest, kernel and rootfs included, so hluk takes
	// no kernel, initrd or scratch memory with it, only what to run in it.
	if args.SnapshotPath != "" {
		cmdArgs := []string{h.binaryPath, "snapshot", "run", args.SnapshotPath}
		cmdArgs = append(cmdArgs, extraMonArgs.OtherArgs...)
		return cmdArgs, nil
	}
	initrdPath := args.InitrdPath
	if initrdPath == "" {
		initrdPath = extraMonArgs.ExtraInitrd
	}
	// Without an initrd hluk has nothing to boot but a kernel, so fail
	// here rather than let the guest start with no filesystem.
	if initrdPath == "" {
		return nil, ErrHyperlightNoInitrd
	}

	cmdArgs := []string{h.binaryPath, "run"}
	if args.UnikernelPath != "" {
		cmdArgs = append(cmdArgs, "--kernel", args.UnikernelPath)
	}
	cmdArgs = append(cmdArgs, "--initrd", initrdPath)
	// hluk sizes the guest by its scratch memory in MiB. A limit too small
	// to express in MiB is left to hluk's own default.
	if memMiB := bytesToMiB(args.MemSizeB); memMiB > 0 {
		cmdArgs = append(cmdArgs, "--scratch-mb", strconv.FormatUint(memMiB, 10))
	}
	cmdArgs = append(cmdArgs, extraMonArgs.OtherArgs...)

	return cmdArgs, nil
}

// PreExec performs pre-execution setup. Hyperlight has no special pre-exec requirements.
func (h *Hyperlight) PreExec(_ types.ExecArgs) error {
	return nil
}
