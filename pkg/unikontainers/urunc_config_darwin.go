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

package unikontainers

import (
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const UruncConfigPath = ""

// UruncConfig holds the urunc configuration
type UruncConfig struct {
	Monitors map[string]types.MonitorConfig
	// ExtraBins mirrors the Linux config's extra-binary map (e.g. virtiofsd)
	// so the shared guest-rootfs selector (ChooseRootfs) compiles and behaves
	// identically. On darwin it is normally empty, so virtiofs is skipped and
	// the selector falls through to 9pfs.
	ExtraBins map[string]types.ExtraBinConfig
}

// LoadUruncConfig loads the urunc configuration
// On darwin, this returns a basic config with reasonable defaults
func LoadUruncConfig(path string) (*UruncConfig, error) {
	return &UruncConfig{
		Monitors: map[string]types.MonitorConfig{
			"qemu-hvf": {
				DefaultMemoryMB: 512,
				DefaultVCPUs:    1,
			},
		},
	}, nil
}
