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
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestConfineBlockSources(t *testing.T) {
	tests := []struct {
		name     string
		blocks   []types.BlockDevParams
		expected []string
	}{
		{
			name:     "explicit block image as the guest rootfs is confined",
			blocks:   []types.BlockDevParams{{ID: "rootfs", Source: "/rootfs.ext2", IsExplicit: true}},
			expected: []string{containerRootfsMountPath + "/rootfs.ext2"},
		},
		{
			name:     "container rootfs as a block device keeps the device path",
			blocks:   []types.BlockDevParams{{ID: "rootfs", Source: "/dev/mapper/snap", IsExplicit: false}},
			expected: []string{"/dev/mapper/snap"},
		},
		{
			name: "block volumes from the container mounts keep the device path",
			blocks: []types.BlockDevParams{
				{ID: "rootfs", Source: "/dev/mapper/snap", IsExplicit: false},
				{ID: "vol1", Source: "/dev/loop3", IsExplicit: false},
			},
			expected: []string{"/dev/mapper/snap", "/dev/loop3"},
		},
		{
			name:     "block image from the annotation without a guest rootfs is confined",
			blocks:   []types.BlockDevParams{{ID: "annot_vol", Source: "/data/vol.ext2", IsExplicit: true}},
			expected: []string{containerRootfsMountPath + "/data/vol.ext2"},
		},
		{
			name:     "parent references are clamped under the container rootfs",
			blocks:   []types.BlockDevParams{{ID: "rootfs", Source: "../../etc/shadow", IsExplicit: true}},
			expected: []string{containerRootfsMountPath + "/etc/shadow"},
		},
		{
			name:     "no block devices",
			blocks:   nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := confineBlockSources(tt.blocks)
			require.NoError(t, err)

			var sources []string
			for _, b := range got {
				sources = append(sources, b.Source)
			}
			assert.Equal(t, tt.expected, sources)
		})
	}
}

// The paths come from the image's annotations, so every one of them has to end
// up under the container rootfs mount, no matter how it is written.
func TestConfineToContainerRootfs(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
		wantErr  bool
	}{
		{name: "empty path stays empty", path: "", expected: ""},
		{name: "absolute path", path: "/unikernel/kernel", expected: containerRootfsMountPath + "/unikernel/kernel"},
		{name: "relative path", path: "unikernel/kernel", expected: containerRootfsMountPath + "/unikernel/kernel"},
		{name: "parent references are clamped", path: "/../../etc/passwd", expected: containerRootfsMountPath + "/etc/passwd"},
		{name: "root of the image", path: "/", expected: containerRootfsMountPath},
		{name: "comma is rejected", path: "/img,readonly=on", wantErr: true},
		{name: "equals is rejected", path: "/img=evil", wantErr: true},
		{name: "space is rejected", path: "/foo bar", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := confineToContainerRootfs(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// Every monitor gets the memory limit of the container. Without one, the
// others fall back to the configured default, while hyperlight-unikraft
// leaves the size to hluk, which knows what the runtime of the rootfs needs.
func TestGuestMemory(t *testing.T) {
	t.Parallel()

	const limitB = 1536 * 1024 * 1024
	limit := int64(limitB)
	limited := &specs.LinuxResources{Memory: &specs.LinuxMemory{Limit: &limit}}
	unlimited := &specs.LinuxResources{Memory: &specs.LinuxMemory{}}
	hyperlight := string(hypervisors.HyperlightVmm)
	qemu := string(hypervisors.QemuVmm)

	tests := []struct {
		name      string
		vmm       string
		resources *specs.LinuxResources
		expected  uint64
	}{
		{name: "qemu with a limit", vmm: qemu, resources: limited, expected: limitB},
		{name: "qemu without a limit", vmm: qemu, resources: unlimited, expected: 256 * 1024 * 1024},
		{name: "qemu without resources", vmm: qemu, resources: nil, expected: 256 * 1024 * 1024},
		{name: "hyperlight-unikraft with a limit", vmm: hyperlight, resources: limited, expected: limitB},
		{name: "hyperlight-unikraft without a limit", vmm: hyperlight, resources: unlimited, expected: 0},
		{name: "hyperlight-unikraft without resources", vmm: hyperlight, resources: nil, expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defaultMem := guestDefaultMemoryMB(tt.vmm, 256)
			assert.Equal(t, tt.expected, monitorMemoryBytes(defaultMem, tt.resources))
		})
	}
}

func TestMonitorVCPUs(t *testing.T) {
	t.Parallel()

	cpu := func(quota int64, period uint64) *specs.LinuxResources {
		return &specs.LinuxResources{CPU: &specs.LinuxCPU{Quota: &quota, Period: &period}}
	}
	noPeriod := int64(200000)
	qemu := string(hypervisors.QemuVmm)
	fc := string(hypervisors.FirecrackerVmm)

	tests := []struct {
		name      string
		def       uint
		guest     string
		vmm       string
		resources *specs.LinuxResources
		hostCPUs  int
		expected  uint
	}{
		{name: "linux without resources", def: 2, guest: "linux", vmm: qemu, expected: 2},
		{name: "linux without cpu section", def: 2, guest: "linux", vmm: qemu, resources: &specs.LinuxResources{}, expected: 2},
		{name: "linux without a quota", def: 2, guest: "linux", vmm: qemu, resources: &specs.LinuxResources{CPU: &specs.LinuxCPU{}}, hostCPUs: 8, expected: 2},
		{name: "linux with an unlimited quota", def: 1, guest: "linux", vmm: qemu, resources: cpu(-1, 100000), hostCPUs: 8, expected: 1},
		{name: "linux with a quota and no period", def: 1, guest: "linux", vmm: qemu, resources: &specs.LinuxResources{CPU: &specs.LinuxCPU{Quota: &noPeriod}}, hostCPUs: 8, expected: 1},
		{name: "linux with a zero period", def: 1, guest: "linux", vmm: qemu, resources: cpu(200000, 0), hostCPUs: 8, expected: 1},
		{name: "linux with two cpus", def: 1, guest: "linux", vmm: qemu, resources: cpu(200000, 100000), hostCPUs: 8, expected: 2},
		{name: "linux with half a cpu", def: 1, guest: "linux", vmm: qemu, resources: cpu(50000, 100000), hostCPUs: 8, expected: 1},
		{name: "linux rounds up", def: 1, guest: "linux", vmm: qemu, resources: cpu(250000, 100000), hostCPUs: 8, expected: 3},
		{name: "limit replaces a larger default", def: 4, guest: "linux", vmm: qemu, resources: cpu(100000, 100000), hostCPUs: 8, expected: 1},
		{name: "limited by the host cpus", def: 1, guest: "linux", vmm: qemu, resources: cpu(1600000, 100000), hostCPUs: 4, expected: 4},
		{name: "firecracker maximum", def: 1, guest: "linux", vmm: fc, resources: cpu(6400000, 100000), hostCPUs: 64, expected: 32},
		{name: "qemu above the firecracker maximum", def: 1, guest: "linux", vmm: qemu, resources: cpu(6400000, 100000), hostCPUs: 64, expected: 64},
		{name: "zero default is one", def: 0, guest: "linux", vmm: qemu, expected: 1},
		{name: "other guests ignore the quota", def: 1, guest: "unikraft", vmm: qemu, resources: cpu(400000, 100000), hostCPUs: 8, expected: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, monitorVCPUs(tt.def, tt.guest, tt.vmm, tt.resources, tt.hostCPUs))
		})
	}
}

func TestExecuteHooksSequential(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "trace")

	u := &Unikontainer{
		State: &specs.State{ID: "test"},
		Spec: &specs.Spec{
			Hooks: &specs.Hooks{
				CreateRuntime: []specs.Hook{
					{Path: "/bin/sh", Args: []string{"sh", "-c", "printf 1 >> " + trace}},
					{Path: "/bin/sh", Args: []string{"sh", "-c", "printf 2 >> " + trace}},
					{Path: "/bin/sh", Args: []string{"sh", "-c", "exit 1"}},
					{Path: "/bin/sh", Args: []string{"sh", "-c", "printf 3 >> " + trace}},
				},
			},
		},
	}

	err := u.ExecuteHooks("CreateRuntime")
	require.Error(t, err)

	out, err := os.ReadFile(trace)
	require.NoError(t, err)
	assert.Equal(t, "12", string(out))
}

// noTapVMM is a monitor that opens the guest's sockets itself, with no tap.
type noTapVMM struct{ types.VMM }

func (noTapVMM) UsesTUNTAP() bool { return false }

// Only a monitor that serves the guest's sockets itself gets the container's
// own resolv.conf mount in its rootfs, read-only, and only when there is one.
func TestMonitorResolvConfMount(t *testing.T) {
	t.Parallel()
	mounts := []specs.Mount{{Type: "bind", Destination: "/etc/resolv.conf/", Source: "/run/engine/resolv.conf", Options: []string{"rbind", "rprivate"}}}

	m, ok := monitorResolvConfMount(noTapVMM{}, mounts)
	require.True(t, ok)
	assert.Equal(t, "bind", m.Type)
	assert.Equal(t, "/run/engine/resolv.conf", m.Source)
	assert.Equal(t, types.ResolvConfPath, m.Destination)
	assert.Equal(t, []string{"rbind", "rprivate", "ro"}, m.Options)
	assert.Equal(t, []string{"rbind", "rprivate"}, mounts[0].Options, "the container's mount is left as it is")

	_, ok = monitorResolvConfMount(noTapVMM{}, nil)
	assert.False(t, ok, "a container without a resolv.conf mounts none")
	_, ok = monitorResolvConfMount(&hypervisors.Qemu{}, mounts)
	assert.False(t, ok, "a tap-based monitor mounts none")
}
