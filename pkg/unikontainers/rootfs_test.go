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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

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
