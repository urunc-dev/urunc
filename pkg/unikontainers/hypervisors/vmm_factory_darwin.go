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

//go:build darwin

package hypervisors

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// QemuHvfAlias is the monitor name hull records in image annotations for
// QEMU with HVF. It resolves to the same factory as "qemu".
const QemuHvfAlias VmmType = "qemu-hvf"

var qemuDarwinFactory = VMMFactory{
	binary:   "qemu-system-aarch64",
	precheck: requireHVF,
	pathFunc: darwinQemuPath,
	createFunc: func(_, binaryPath string, _ bool) types.VMM {
		return NewQemuDarwin(binaryPath)
	},
}

// vmmFactories holds the monitors urunc can start on darwin: QEMU with HVF,
// Virtualization.framework through vz-runner, and hvi on
// Hypervisor.framework. The Linux-only monitors
// (spt, hvt, firecracker, cloud-hypervisor, hedge, hyperlight) are absent.
var vmmFactories = map[VmmType]VMMFactory{
	QemuVmm:      qemuDarwinFactory,
	QemuHvfAlias: qemuDarwinFactory,
	VzVmm: {
		precheck: func() error { return NewVzDarwin().Ok() },
		createFunc: func(_, _ string, _ bool) types.VMM {
			return NewVzDarwin()
		},
	},
	HviVmm: {
		binary:   HviBinary,
		pathFunc: darwinHviPath,
		createFunc: func(_, binaryPath string, _ bool) types.VMM {
			return NewHviDarwin(binaryPath)
		},
	},
}

func requireHVF() error {
	if !IsHVFAvailable() {
		return errors.New("HVF acceleration not available on this system (requires Apple Silicon)")
	}
	return nil
}

// darwinQemuPath resolves the qemu-system-aarch64 binary: the monitor config
// path (under "qemu", then "qemu-hvf") first, then re-signed copies in /tmp
// or /usr/local/bin (once needed for the hypervisor entitlement), then PATH.
func darwinQemuPath(monitors map[string]types.MonitorConfig) (string, error) {
	for _, name := range []VmmType{QemuVmm, QemuHvfAlias} {
		if p := monitors[string(name)].BinaryPath; p != "" {
			return p, nil
		}
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

// darwinHviPath resolves the hvi binary: the monitor config path first, then
// an executable hvi next to the running binary (where hull bundles its signed
// helpers, as it does vz-runner), then PATH.
func darwinHviPath(monitors map[string]types.MonitorConfig) (string, error) {
	if p := monitors[string(HviVmm)].BinaryPath; p != "" {
		return p, nil
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), HviBinary)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	if p, err := exec.LookPath(HviBinary); err == nil {
		return p, nil
	}
	return "", errors.New("hvi not found next to the executable or in PATH")
}
