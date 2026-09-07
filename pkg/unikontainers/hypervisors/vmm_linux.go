//go:build linux
// +build linux

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

import "github.com/urunc-dev/urunc/pkg/unikontainers/types"

// vmmFactories is the Linux monitor registry consumed by the shared NewVMM.
var vmmFactories = map[VmmType]VMMFactory{
	SptVmm: {
		binary: SptBinary,
		createFunc: func(binary, binaryPath string, _ bool) types.VMM {
			return &SPT{binary: binary, binaryPath: binaryPath}
		},
	},
	HvtVmm: {
		binary: HvtBinary,
		createFunc: func(binary, binaryPath string, _ bool) types.VMM {
			return &HVT{binary: binary, binaryPath: binaryPath}
		},
	},
	QemuVmm: {
		binary: QemuBinary,
		createFunc: func(binary, binaryPath string, vhost bool) types.VMM {
			return &Qemu{binary: binary, binaryPath: binaryPath, vhost: vhost}
		},
	},
	FirecrackerVmm: {
		binary: FirecrackerBinary,
		createFunc: func(binary, binaryPath string, _ bool) types.VMM {
			return &Firecracker{binary: binary, binaryPath: binaryPath}
		},
	},
	CloudHypervisorVmm: {
		binary: CloudHypervisorBinary,
		createFunc: func(binary, binaryPath string, _ bool) types.VMM {
			return &CloudHypervisor{binary: binary, binaryPath: binaryPath}
		},
	},
	HyperlightVmm: {
		binary: HyperlightBinary,
		createFunc: func(binary, binaryPath string, _ bool) types.VMM {
			return &Hyperlight{binary: binary, binaryPath: binaryPath}
		},
	},
	// Hedge has no external binary; it checks for the hedge KVM extension.
	// Folding it into the registry (precheck + createFunc) removes the
	// special-case NewVMM used to carry for it.
	HedgeVmm: {
		precheck: func() error {
			h := Hedge{}
			if err := h.Ok(); err != nil {
				return ErrVMMNotInstalled
			}
			return nil
		},
		createFunc: func(_, _ string, _ bool) types.VMM {
			return &Hedge{}
		},
	},
}
