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
	"errors"
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

// hvtVMM is a Solo5 hvt monitor, which needs neither KVM nor a tap in the tests.
type hvtVMM struct{ types.VMM }

func (hvtVMM) Path() string                { return "/usr/local/bin/" + hypervisors.HvtBinary }
func (hvtVMM) UsesKVM() bool               { return false }
func (hvtVMM) UsesTUNTAP() bool            { return false }
func (hvtVMM) SupportsControlSocket() bool { return false }

// socketVMM is a monitor like hvtVMM, but with a control socket.
type socketVMM struct{ hvtVMM }

func (socketVMM) SupportsControlSocket() bool { return true }

// fakeRootfs is a rootfs builder that adds only the given mounts and block
// volumes, or fails with blockErr to get the block volumes.
type fakeRootfs struct {
	rootfsBuilder
	mounts    []specs.Mount
	blockArgs []types.BlockDevParams
	blockErr  error
}

func (f fakeRootfs) getMounts() ([]specs.Mount, error)             { return f.mounts, nil }
func (f fakeRootfs) getBlockDevs() ([]types.BlockDevParams, error) { return f.blockArgs, f.blockErr }
func (fakeRootfs) getSharedDirs() (types.SharedfsParams, error)    { return types.SharedfsParams{}, nil }
func (fakeRootfs) preStartCmd() []string                           { return nil }

// The host libraries are mounted first and then the monitor binary, so that the
// rest of the mounts hide them if they overlap. The rest of the mounts of the
// monitor come before the mounts of the rootfs. The host libraries are checked
// against everything else that urunc adds in the monitor rootfs.
func TestGetMonitorResourcesHostLibs(t *testing.T) {
	t.Parallel()

	share := t.TempDir()
	lib := t.TempDir()
	rfs := fakeRootfs{mounts: []specs.Mount{bindMount(t.TempDir(), filepath.Join(share, "rootfs"), true, false)}}

	res, err := getMonitorResources(rfs, hvtVMM{}, hypervisors.HvtVmm, types.MonitorConfig{HostLibs: []string{lib}}, nil)
	require.NoError(t, err)
	var got []string
	for _, m := range res.Mounts {
		got = append(got, m.Destination)
	}
	want := []string{lib, hvtVMM{}.Path(), "/proc", "/dev", "/dev/pts", filepath.Join(share, "rootfs")}
	assert.Equal(t, want, got)

	invalid := []struct {
		lib  string
		want string
	}{
		// A mount of the rootfs.
		{share, "overlaps " + filepath.Join(share, "rootfs") + ","},
		// The devices are under /dev.
		{"/dev/null", "overlaps /dev,"},
		{"/dev/shm", "overlaps /dev,"},
		// Paths that urunc writes before the mounts.
		{containerRootfsMountPath, "overlaps " + containerRootfsMountPath + ","},
		{"/" + monitorSpecFilename, "overlaps /" + monitorSpecFilename + ","},
	}
	for _, tc := range invalid {
		_, err = getMonitorResources(rfs, hvtVMM{}, hypervisors.HvtVmm, types.MonitorConfig{HostLibs: []string{tc.lib}}, nil)
		assert.ErrorContains(t, err, tc.want, tc.lib)
	}
}

// The host libraries of virtiofsd are added only for a virtiofs rootfs, where
// virtiofsd runs in the monitor rootfs too. The configuration is not modified.
func TestMonitorHostLibs(t *testing.T) {
	t.Parallel()

	// qemuLibs uses the first element of qemuLibsArray and has the second one
	// as spare capacity, so a plain append would write into the second one.
	qemuLibsArray := [2]string{"/usr/lib"}
	qemuLibs := qemuLibsArray[:1]
	cfg := &UruncConfig{
		Monitors:  map[string]types.MonitorConfig{"qemu": {HostLibs: qemuLibs}},
		ExtraBins: map[string]types.ExtraBinConfig{"virtiofsd": {HostLibs: []string{"/lib"}}},
	}

	assert.Equal(t, []string{"/usr/lib"}, monitorHostLibs(cfg, "qemu", "block"))
	assert.Equal(t, []string{"/usr/lib"}, monitorHostLibs(cfg, "qemu", "9pfs"))
	assert.Equal(t, []string{"/usr/lib", "/lib"}, monitorHostLibs(cfg, "qemu", "virtiofs"))
	assert.Empty(t, qemuLibsArray[1], "the libraries of the configuration were modified")
	assert.Nil(t, monitorHostLibs(cfg, "hvt", "block"))
}

// The control socket of the monitor is reserved, since a library that contains
// it would make its directory read-only, but only for a monitor that has one.
func TestGetMonitorResourcesControlSocket(t *testing.T) {
	t.Parallel()

	lib := t.TempDir()
	socket := filepath.Join(lib, "urunc", "api.sock")
	rfs := fakeRootfs{}

	_, err := getMonitorResources(rfs, socketVMM{}, hypervisors.FirecrackerVmm, types.MonitorConfig{SocketPath: socket, HostLibs: []string{lib}}, nil)
	assert.ErrorContains(t, err, "overlaps "+socket+",")

	// The socket's directory is not reserved, which could be / and reject
	// every library.
	_, err = getMonitorResources(rfs, socketVMM{}, hypervisors.FirecrackerVmm, types.MonitorConfig{SocketPath: "/api.sock", HostLibs: []string{lib}}, nil)
	assert.NoError(t, err)

	// A monitor without a control socket ignores the path.
	_, err = getMonitorResources(rfs, hvtVMM{}, hypervisors.HvtVmm, types.MonitorConfig{SocketPath: socket, HostLibs: []string{lib}}, nil)
	assert.NoError(t, err)
}

// getMonitorResources changes nothing on the host, so it does not get the
// block devices, which unmounts the block volumes of the container.
func TestGetMonitorResourcesSkipsBlockDevices(t *testing.T) {
	t.Parallel()

	rfs := fakeRootfs{blockErr: errors.New("block volumes unmounted")}
	res, err := getMonitorResources(rfs, hvtVMM{}, hypervisors.HvtVmm, types.MonitorConfig{}, nil)
	require.NoError(t, err)
	assert.Empty(t, res.BlockArgs)
}

// If blockDevNodes fails after getBlockDevs unmounted the block volumes, they
// are mounted back, since Delete can restore them only after InitialSetup has
// finished.
func TestGetBlockResources(t *testing.T) {
	t.Parallel()

	// The source of the volume does not exist. So blockDevNodes finds no
	// device node, and the volume can not be mounted back. The error still
	// shows that the remount was tried, even without privileges.
	vol := types.BlockDevParams{
		Source:         filepath.Join(t.TempDir(), "loop"),
		HostMountPoint: t.TempDir(),
		FsType:         "ext4",
	}
	rfs := fakeRootfs{blockArgs: []types.BlockDevParams{vol}}
	_, _, err := getBlockResources(rfs, types.RootfsParams{Type: "block"})
	assert.ErrorContains(t, err, "failed to get the device node of "+vol.Source+": ")
	assert.ErrorContains(t, err, ", and failed to mount the block volumes back: failed to remount "+vol.Source)

	// A volume without a host mountpoint is not mounted back, so the error
	// names only the device node.
	vol.HostMountPoint = ""
	rfs = fakeRootfs{blockArgs: []types.BlockDevParams{vol}}
	_, _, err = getBlockResources(rfs, types.RootfsParams{Type: "block"})
	assert.ErrorContains(t, err, "failed to get the device node of "+vol.Source+": ")
	assert.NotContains(t, err.Error(), "mount the block volumes back")

	// Without an error, the volumes stay unmounted for the guest.
	blockArgs, blockDevs, err := getBlockResources(rfs, types.RootfsParams{})
	require.NoError(t, err)
	assert.Equal(t, []types.BlockDevParams{vol}, blockArgs)
	assert.Empty(t, blockDevs)

	rfs = fakeRootfs{blockErr: errors.New("unmount failed")}
	_, _, err = getBlockResources(rfs, types.RootfsParams{})
	assert.EqualError(t, err, "failed to get the block devices of the guest: unmount failed")
}

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
