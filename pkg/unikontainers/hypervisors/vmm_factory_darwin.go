//go:build darwin
// +build darwin

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
	"os"
	"os/exec"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// vmmFactories is the macOS monitor registry consumed by the shared NewVMM.
// Only QEMU (HVF) and Virtualization.framework are available; the Linux-only
// monitors (SPT/HVT/Firecracker/CloudHypervisor/Hedge) are absent.
// QemuHvfAlias is the hypervisor name the macOS product records in image
// annotations for the QEMU/HVF backend; it resolves to the same factory as
// the canonical "qemu".
const QemuHvfAlias VmmType = "qemu-hvf"

var qemuDarwinFactory = VMMFactory{
	binary:   "qemu-system-aarch64",
	precheck: requireHVF,
	pathFunc: darwinQemuPath,
	createFunc: func(_, binaryPath string, _ bool) types.VMM {
		return NewQemuDarwin(binaryPath)
	},
}

var vmmFactories = map[VmmType]VMMFactory{
	QemuVmm:      qemuDarwinFactory,
	QemuHvfAlias: qemuDarwinFactory,
	VzVmm: {
		precheck: func() error { return NewVzDarwin().Ok() },
		createFunc: func(_, _ string, _ bool) types.VMM {
			return NewVzDarwin()
		},
	},
}

func requireHVF() error {
	if !IsHVFAvailable() {
		return errors.New("HVF acceleration not available on this system (requires Apple Silicon)")
	}
	return nil
}

// darwinQemuPath resolves the qemu-system-aarch64 binary, honoring an explicit
// monitor config path first, then preferring re-signed copies in /tmp or
// /usr/local/bin (historically needed for the hypervisor entitlement), then
// falling back to PATH.
func darwinQemuPath(monitors map[string]types.MonitorConfig) (string, error) {
	if p := monitors[string(QemuVmm)].BinaryPath; p != "" {
		return p, nil
	}
	for _, candidate := range []string{
		"/tmp/qemu-system-aarch64",
		"/usr/local/bin/qemu-system-aarch64",
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	p, err := exec.LookPath("qemu-system-aarch64")
	if err != nil {
		return "", errors.New("qemu-system-aarch64 not found in PATH; install with: brew install qemu")
	}
	return p, nil
}
