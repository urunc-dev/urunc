//go:build cgo

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

// This file holds the parts of the libcontainer monitor path that import runc's
// specconv and libcontainer packages, both of which require cgo. urunc is always
// built with cgo, so it always has them. The containerd shim is built without
// cgo and never uses the libcontainer runtime path, so gating these behind the
// cgo build tag keeps the shim free of the cgo-only dependency. See
// libcontainer_nocgo.go for the non-cgo stub that lets Delete still link.

package unikontainers

import (
	"fmt"
	"path/filepath"

	"github.com/opencontainers/cgroups"
	// Registers the cgroup device-rule setter (DevicesSetV1/V2). Without this
	// blank import the setter stays nil and starting a container with device
	// rules fails with "cgroup manager is not configured to set device rules".
	_ "github.com/opencontainers/cgroups/devices"
	devices "github.com/opencontainers/cgroups/devices/config"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/specconv"
)

// BuildContainerConfig modifies the container's OCI spec, according to the monitor
// mounts and devices gathered during InitialSetup
func (u *Unikontainer) BuildContainerConfig(systemdCgroup bool) (*configs.Config, error) {
	monRes, err := loadMonitorResources(u.BaseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load monitor resources: %w", err)
	}

	monSpec := monitorOCISpec(u.Spec, monRes, monRes.Rootfs)

	config, err := specconv.CreateLibcontainerConfig(&specconv.CreateOpts{
		CgroupName:       u.State.ID,
		UseSystemdCgroup: systemdCgroup,
		Spec:             monSpec,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build the monitor's libcontainer config: %w", err)
	}

	// urunc applies monitor-specific seccomp filters, therefore libcontainer
	// should not apply any.
	config.Seccomp = nil

	// Allow all devices through cgroups to avoid issues with device access from
	// "urunc init" or the monitor process.
	// TODO: Revisit this in the future and apply a stricter model.
	config.Cgroups.Resources = &cgroups.Resources{
		Devices: []*devices.Rule{{Type: devices.WildcardDevice, Allow: true}},
	}

	// The Prestart, CreateRuntime, CreateContainer and StartContainer
	// hooks are part of the configuration and are libcontainer's to run,
	// Poststart and Poststop stay with urunc, in start and delete
	// respectively, due to the extra steps urunc performs (sandbox and
	// network cleanup).
	delete(config.Hooks, configs.Poststart)
	delete(config.Hooks, configs.Poststop)

	return config, nil
}

// LibcontainerRoot returns the state directory libcontainer uses for the
// monitor's process execution environment. It is placed under the root directory
func (u *Unikontainer) LibcontainerRoot() string {
	return filepath.Join(u.RootDir, libcontainerDirName)
}
