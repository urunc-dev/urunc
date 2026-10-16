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
	"fmt"
	"slices"

	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// monCaps all the necessary capabilities for the "urunc init" process
// to finalize the setup of the monitor's execution environment.
//   - CAP_NET_ADMIN: required for the creation and setup of th the tap device
//     inside the container's network namespace.
var monCaps = []string{"CAP_NET_ADMIN"}

// monitorOCISpec builds the OCI spec that describes the monitor's
// execution environment. It consists of the container's spec,
// but altered accordingly for the respective monitor and rootfs.
func monitorOCISpec(spec *specs.Spec, monRes monitorResources, rootfsParams types.RootfsParams) *specs.Spec {
	monSpec := *spec
	monSpec.Root = &specs.Root{Path: rootfsParams.MonRootfs, Readonly: false}
	monSpec.Mounts = monRes.Mounts

	process := *spec.Process
	process.Cwd = "/"
	monSpec.Process = &process

	linux := *spec.Linux
	linux.Devices = monRes.Devices
	linux.Namespaces = spec.Linux.Namespaces
	monSpec.Linux = &linux

	return &monSpec
}

// MonitorCapabilities returns the capability set the monitor process of the
// container requires; see monitorCapabilities.
func (u *Unikontainer) MonitorCapabilities() (*configs.Capabilities, error) {
	monRes, err := loadMonitorResources(u.BaseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load monitor resources: %w", err)
	}
	return monitorCapabilities(u.Spec, monRes.TUNTAP), nil
}

// monitorCapabilities returns the capability set the monitor process requires
// to finalize the execution environment for the monitor. Currently, it consists
// of the container's own set plus CAP_NET_ADMIN in all five sets for a monitor
// that creates a tap. A monitor without one gets the container's own set.
// TODO: Narrow down to the extremely necessary capabilities.
func monitorCapabilities(spec *specs.Spec, tuntap bool) *configs.Capabilities {
	caps := specCapabilities(spec)
	if caps == nil {
		uniklog.Warn("The container declares no capabilities. The monitor inherits urunc's own set.")
		return nil
	}
	if !tuntap {
		return caps
	}

	for _, capab := range monCaps {
		caps.Bounding = withCap(caps.Bounding, capab)
		caps.Effective = withCap(caps.Effective, capab)
		caps.Permitted = withCap(caps.Permitted, capab)
		caps.Inheritable = withCap(caps.Inheritable, capab)
		caps.Ambient = withCap(caps.Ambient, capab)
	}

	return caps
}

// specCapabilities copies the container's capability set out of the OCI spec,
// returning nil when the spec declares none.
func specCapabilities(spec *specs.Spec) *configs.Capabilities {
	if spec.Process == nil || spec.Process.Capabilities == nil {
		return nil
	}

	caps := spec.Process.Capabilities

	return &configs.Capabilities{
		Bounding:    slices.Clone(caps.Bounding),
		Effective:   slices.Clone(caps.Effective),
		Permitted:   slices.Clone(caps.Permitted),
		Inheritable: slices.Clone(caps.Inheritable),
		Ambient:     slices.Clone(caps.Ambient),
	}
}

// withCap returns caps with add appended, unless it is already there.
func withCap(caps []string, add string) []string {
	if slices.Contains(caps, add) {
		return caps
	}

	return append(caps, add)
}
