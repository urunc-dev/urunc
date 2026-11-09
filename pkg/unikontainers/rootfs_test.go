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
	"strings"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestNewRootfsResult(t *testing.T) {
	expected := types.RootfsParams{
		Type:        "initrd",
		Path:        "/path/to/initrd",
		MountedPath: "/mnt/rootfs",
	}

	got := newRootfsResult("initrd", "/path/to/initrd", "/mnt/rootfs")

	assert.Equal(t, expected.Type, got.Type, "Type should match")
	assert.Equal(t, expected.Path, got.Path, "Path should match")
	assert.Equal(t, expected.MountedPath, got.MountedPath, "MountedPath should match")
	// MonRootfs is intentionally not set by newRootfsResult; switchMonRootfs owns it.
	assert.Empty(t, got.MonRootfs, "MonRootfs should be left unset")
}

func TestRootfsSelector_TryInitrd(t *testing.T) {
	tests := []struct {
		name          string
		annot         map[string]string
		expectedFound bool
		expectedType  string
		expectedPath  string
	}{
		{
			name: "initrd present",
			annot: map[string]string{
				annotInitrd: "/path/to/initrd.img",
			},
			expectedFound: true,
			expectedType:  "initrd",
			expectedPath:  "/path/to/initrd.img",
		},
		{
			name:          "initrd missing",
			annot:         map[string]string{},
			expectedFound: false,
		},
		{
			name: "initrd empty",
			annot: map[string]string{
				annotInitrd: "",
			},
			expectedFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rs := &rootfsSelector{
				annot:      tt.annot,
				cntrRootfs: "/container/rootfs",
			}

			got, found := rs.tryInitrd()

			assert.Equal(t, tt.expectedFound, found, "tryInitrd() found mismatch")

			if found {
				assert.Equal(t, tt.expectedType, got.Type, "tryInitrd() Type mismatch")
				assert.Equal(t, tt.expectedPath, got.Path, "tryInitrd() Path mismatch")
			}
		})
	}
}

func TestRootfsSelector_ShouldMountContainerRootfs(t *testing.T) {
	tests := []struct {
		name     string
		annot    map[string]string
		expected bool
	}{
		{
			name: "mount rootfs true",
			annot: map[string]string{
				annotMountRootfs: "true",
			},
			expected: true,
		},
		{
			name: "mount rootfs 1",
			annot: map[string]string{
				annotMountRootfs: "1",
			},
			expected: true,
		},
		{
			name: "mount rootfs false",
			annot: map[string]string{
				annotMountRootfs: "false",
			},
			expected: false,
		},
		{
			name: "mount rootfs 0",
			annot: map[string]string{
				annotMountRootfs: "0",
			},
			expected: false,
		},
		{
			name:     "mount rootfs missing",
			annot:    map[string]string{},
			expected: false,
		},
		{
			name: "mount rootfs empty",
			annot: map[string]string{
				annotMountRootfs: "",
			},
			expected: false,
		},
		{
			name: "mount rootfs invalid",
			annot: map[string]string{
				annotMountRootfs: "invalid",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rs := &rootfsSelector{
				annot: tt.annot,
			}

			got := rs.shouldMountContainerRootfs()
			assert.Equal(t, tt.expected, got, "shouldMountContainerRootfs() mismatch")
		})
	}
}

func TestMonitorDeviceMode(t *testing.T) {
	t.Run("widens a group-only device for other users", func(t *testing.T) {
		t.Parallel()
		// /dev/kvm on a stock host: 0660 root:kvm. Without the widening a
		// monitor running as a non-root user gets EACCES on it.
		assert.Equal(t, os.FileMode(0o666), monitorDeviceMode(0o660))
	})

	t.Run("leaves an already-wide device alone", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, os.FileMode(0o666), monitorDeviceMode(0o666))
	})

	t.Run("keeps the owner and group bits", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, os.FileMode(0o646), monitorDeviceMode(0o640))
	})

	t.Run("drops the file type bits", func(t *testing.T) {
		t.Parallel()
		// unix.Stat reports the type in the high bits; only the permission
		// bits may reach mknod.
		assert.Equal(t, os.FileMode(0o666), monitorDeviceMode(unix.S_IFCHR|0o660))
	})
}

func TestNoRootfsGetMounts(t *testing.T) {
	t.Run("mounts the container rootfs read-only without a block image", func(t *testing.T) {
		t.Parallel()
		n := noRootfs{containerRootfsPath: "/run/rootfs"}

		mounts, err := n.getMounts()
		require.NoError(t, err)
		require.Len(t, mounts, 2)
		assert.Equal(t, "/run/rootfs", mounts[0].Source)
		assert.Equal(t, containerRootfsMountPath, mounts[0].Destination)
		assert.Contains(t, mounts[0].Options, "ro")
	})

	t.Run("keeps the container rootfs writable when a block image is attached", func(t *testing.T) {
		t.Parallel()
		// The monitor opens the image read-write, so the mount that holds it
		// can not be read-only.
		n := noRootfs{
			containerRootfsPath:  "/run/rootfs",
			annotBlockPath:       "/data/vol.ext2",
			annotBlockMountPoint: "/data",
		}

		mounts, err := n.getMounts()
		require.NoError(t, err)
		require.Len(t, mounts, 2)
		assert.Equal(t, containerRootfsMountPath, mounts[0].Destination)
		assert.NotContains(t, mounts[0].Options, "ro")
	})
}

// A monitor that creates no tap gets no /dev/net/tun in its rootfs.
func TestGetMonitorDevicesTUN(t *testing.T) {
	t.Parallel()

	paths := func(devs []specs.LinuxDevice) []string {
		var p []string
		for _, d := range devs {
			p = append(p, d.Path)
		}
		return p
	}

	devs, err := getMonitorDevices(false, false)
	require.NoError(t, err)
	assert.NotContains(t, paths(devs), "/dev/net/tun")
	assert.Contains(t, paths(devs), "/dev/null")

	if _, err := os.Stat("/dev/net/tun"); err != nil {
		t.Skip("no /dev/net/tun on this host")
	}
	devs, err = getMonitorDevices(false, true)
	require.NoError(t, err)
	assert.Contains(t, paths(devs), "/dev/net/tun")
}

// The host libraries reach the monitor rootfs only as listed in the
// configuration: read-only, nodev and nosuid, but executable for the loader,
// at the same path and once each.
func TestHostLibMounts(t *testing.T) {
	t.Parallel()

	reservedPaths := []string{"/proc", "/dev", "/dev/pts", "/usr/local/bin/" + hypervisors.HvtBinary}
	libDir := t.TempDir()
	usrLibDir := t.TempDir()

	tests := []struct {
		name string
		libs []string
		want []string
	}{
		{"none", nil, nil},
		{"listed", []string{libDir, usrLibDir}, []string{libDir, usrLibDir}},
		{"duplicate", []string{libDir, usrLibDir, libDir}, []string{libDir, usrLibDir}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mounts, err := hostLibMounts(tc.libs, reservedPaths)
			require.NoError(t, err)

			var got []string
			for _, m := range mounts {
				assert.Equal(t, m.Source, m.Destination)
				assert.Subset(t, m.Options, []string{"ro", "nodev", "nosuid"})
				assert.NotContains(t, m.Options, "noexec")
				got = append(got, m.Destination)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// A host library path must be absolute, exist, be a regular file or a
// directory (e.g. not a socket or a fifo), and it can not overlap what urunc
// mounts or creates itself in the monitor rootfs, in either direction.
func TestInvalidHostLibMounts(t *testing.T) {
	t.Parallel()

	reservedPaths := []string{
		"/proc",
		"/dev",
		"/dev/pts",
		"/usr/bin/qemu-system-x86_64",
		"/usr/share/qemu",
		containerRootfsMountPath,
		types.ResolvConfPath,
	}
	fifo := filepath.Join(t.TempDir(), "fifo")
	require.NoError(t, unix.Mkfifo(fifo, 0o600))

	invalid := []struct {
		name string
		lib  string
		want string
	}{
		{"missing", filepath.Join(t.TempDir(), "missing"), "failed to get info"},
		{"fifo", fifo, "neither a regular file nor a directory"},
		{"root", "/", "overlaps /proc,"},
		{"proc", "/proc", "overlaps /proc,"},
		{"inside dev", "/dev/shm", "overlaps /dev,"},
		{"parent of the monitor", "/usr/bin", "overlaps /usr/bin/qemu-system-x86_64,"},
		{"parent of qemu data", "/usr/share", "overlaps /usr/share/qemu,"},
		{"inside qemu data", "/usr/share/qemu/firmware", "overlaps /usr/share/qemu,"},
		{"parent of resolv.conf", "/etc", "overlaps /etc/resolv.conf,"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := hostLibMounts([]string{tc.lib}, reservedPaths)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// A host library path can not contain or be inside another listed one,
// whichever is listed first.
func TestNestedHostLibMounts(t *testing.T) {
	t.Parallel()

	libDir := t.TempDir()
	nestedLib := filepath.Join(libDir, "libc.so.6")
	require.NoError(t, os.WriteFile(nestedLib, nil, 0o600))

	_, err := hostLibMounts([]string{libDir, nestedLib}, nil)
	assert.ErrorContains(t, err, "host library path "+nestedLib+" overlaps the host library path "+libDir)

	_, err = hostLibMounts([]string{nestedLib, libDir}, nil)
	assert.ErrorContains(t, err, "host library path "+libDir+" overlaps the host library path "+nestedLib)
}

// hasPathPrefix compares whole path elements, so a path that only starts with
// the same characters is not under the prefix.
func TestHasPathPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path   string
		prefix string
		want   bool
	}{
		{"/usr/lib", "/usr/lib", true},
		{"/usr/lib/x86_64-linux-gnu", "/usr/lib", true},
		{"/usr/lib64", "/usr/lib", false},
		{"/usr", "/usr/lib", false},
		{"/devices", "/dev", false},
		{"/dev", "/", true},
		{"/", "/", true},
		{"/", "/dev", false},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, hasPathPrefix(tc.path, tc.prefix), "hasPathPrefix(%q, %q)", tc.path, tc.prefix)
	}
}

// Only what urunc adds for the monitor and the container is checked, so e.g. a
// path that overlaps Qemu's data files is valid for another monitor.
func TestHostLibMountsOnlyCheckReservedPaths(t *testing.T) {
	t.Parallel()

	share := t.TempDir()

	_, err := hostLibMounts([]string{share}, []string{filepath.Join(share, "qemu")})
	assert.ErrorContains(t, err, "overlaps")

	mounts, err := hostLibMounts([]string{share}, nil)
	require.NoError(t, err)
	assert.Len(t, mounts, 1)
}

// Qemu's data files reach the monitor rootfs only from the configured
// data_files; urunc does not search the host for them. Each listed file is
// mounted in Qemu's data directory under its own name, while a single listed
// directory is mounted as the whole data directory. A file that is a symlink
// is mounted with its target's content. Every listed path must exist and be
// listed once.
func TestMountsForMonitorQemuDataFiles(t *testing.T) {
	t.Parallel()

	share := t.TempDir()
	qemuDir := filepath.Join(share, "qemu")
	seabiosDir := filepath.Join(share, "seabios")
	require.NoError(t, os.Mkdir(qemuDir, 0o755))
	require.NoError(t, os.Mkdir(seabiosDir, 0o755))
	kvmvapic := filepath.Join(qemuDir, "kvmvapic.bin")
	require.NoError(t, os.WriteFile(kvmvapic, nil, 0o600))
	seabiosBios := filepath.Join(seabiosDir, "bios-256k.bin")
	require.NoError(t, os.WriteFile(seabiosBios, nil, 0o600))
	bios := filepath.Join(qemuDir, "bios-256k.bin")
	require.NoError(t, os.Symlink("../seabios/bios-256k.bin", bios))
	missing := filepath.Join(qemuDir, "missing.rom")

	tests := []struct {
		name  string
		files []string
		want  map[string]string
	}{
		{"none", nil, map[string]string{}},
		{"files", []string{kvmvapic, bios}, map[string]string{
			"/usr/share/qemu/kvmvapic.bin":  kvmvapic,
			"/usr/share/qemu/bios-256k.bin": bios,
		}},
		{"directory", []string{qemuDir}, map[string]string{
			"/usr/share/qemu": qemuDir,
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mounts, err := mountsForMonitor(hypervisors.QemuVmm, "/usr/bin/qemu-system-x86_64", types.MonitorConfig{DataFiles: tc.files}, nil)
			require.NoError(t, err)

			got := map[string]string{}
			for _, m := range mounts {
				if strings.HasPrefix(m.Destination, "/usr/share/") {
					assert.Subset(t, m.Options, []string{"ro", "nodev", "nosuid", "noexec"})
					got[m.Destination] = m.Source
				}
			}
			assert.Equal(t, tc.want, got)
		})
	}

	invalid := []struct {
		name  string
		files []string
	}{
		{"missing", []string{missing, kvmvapic}},
		{"directory and missing file", []string{qemuDir, missing}},
		{"duplicate", []string{kvmvapic, kvmvapic}},
		{"duplicate directory", []string{qemuDir, qemuDir}},
		{"directory and file", []string{qemuDir, seabiosBios}},
		{"two directories", []string{qemuDir, seabiosDir}},
		{"same name", []string{bios, seabiosBios}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := mountsForMonitor(hypervisors.QemuVmm, "/usr/bin/qemu-system-x86_64", types.MonitorConfig{DataFiles: tc.files}, nil)
			assert.Error(t, err)
		})
	}
}

// Only Qemu gets data files. Its type decides that, not the name of its
// binary, which can be anything.
func TestMountsForMonitorQemuDataFilesByType(t *testing.T) {
	t.Parallel()

	dataFile := filepath.Join(t.TempDir(), "bios-256k.bin")
	require.NoError(t, os.WriteFile(dataFile, nil, 0o600))
	cfg := types.MonitorConfig{DataFiles: []string{dataFile}}

	mounts, err := mountsForMonitor(hypervisors.QemuVmm, "/usr/local/bin/urunc-qemu", cfg, nil)
	require.NoError(t, err)
	assert.Contains(t, mountDestinations(mounts), "/usr/share/qemu/bios-256k.bin")

	mounts, err = mountsForMonitor(hypervisors.HvtVmm, "/usr/local/bin/qemu-hvt", cfg, nil)
	require.NoError(t, err)
	assert.NotContains(t, mountDestinations(mounts), "/usr/share/qemu/bios-256k.bin")
}

// mountDestinations returns the destinations of mounts.
func mountDestinations(mounts []specs.Mount) []string {
	var destinations []string
	for _, m := range mounts {
		destinations = append(destinations, m.Destination)
	}
	return destinations
}

// The host libraries can not overlap Qemu's data files, which are mounted in
// /usr/share/qemu.
func TestMountsForMonitorHostLibsAndQemuDataFiles(t *testing.T) {
	t.Parallel()

	dataFile := filepath.Join(t.TempDir(), "kvmvapic.bin")
	require.NoError(t, os.WriteFile(dataFile, nil, 0o600))
	share := t.TempDir()

	cfg := types.MonitorConfig{DataFiles: []string{dataFile}, HostLibs: []string{"/usr/share"}}
	_, err := mountsForMonitor(hypervisors.QemuVmm, "/usr/bin/qemu-system-x86_64", cfg, nil)
	assert.ErrorContains(t, err, "overlaps /usr/share/qemu/kvmvapic.bin,")

	cfg = types.MonitorConfig{DataFiles: []string{dataFile}, HostLibs: []string{share}}
	mounts, err := mountsForMonitor(hypervisors.QemuVmm, "/usr/bin/qemu-system-x86_64", cfg, nil)
	require.NoError(t, err)
	assert.Equal(t, share, mounts[0].Destination, "the host libraries are mounted first")
}
