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

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestSharedfsRootfsGetMounts(t *testing.T) {
	t.Run("9pfs binds the container rootfs and a 9pfs-sized tmpfs, nothing virtiofs-specific", func(t *testing.T) {
		s := sharedfsRootfs{mountedPath: "/mnt/rootfs", sfsType: "9pfs", memory: 256 * 1024 * 1024}
		mounts, err := s.getMounts()
		require.NoError(t, err)
		require.Len(t, mounts, 2)
		assert.Equal(t, containerRootfsMountPath, mounts[0].Destination)
		assert.Equal(t, "/mnt/rootfs", mounts[0].Source)
		assert.Equal(t, "/tmp", mounts[1].Destination)
		assert.Contains(t, mounts[1].Options, "size="+tmpfsSizeFor9pfsRootfs)
	})

	t.Run("virtiofs adds a bind mount for the virtiofsd binary and a memory-sized tmpfs", func(t *testing.T) {
		// Kills a mutant that drops the sfsType == "virtiofs" guard, which
		// would either always or never add the virtiofsd bind mount.
		s := sharedfsRootfs{
			mountedPath: "/mnt/rootfs", sfsType: "virtiofs", memory: 256 * 1024 * 1024,
			vfsdConfig: types.ExtraBinConfig{Path: "/usr/libexec/virtiofsd"},
		}
		mounts, err := s.getMounts()
		require.NoError(t, err)
		require.Len(t, mounts, 3, "rootfs bind, virtiofsd bind, tmpfs")
		assert.Equal(t, "/usr/libexec/virtiofsd", mounts[1].Destination)
		assert.Equal(t, "/usr/libexec/virtiofsd", mounts[1].Source)
		assert.Contains(t, mounts[1].Options, "ro", "the virtiofsd binary itself must be read-only")
		if strictOn("memory-limit") {
			assert.Contains(t, mounts[2].Options, "size=257m", "256MiB guest memory + the fixed 1MiB extra chooseTmpfsSize adds")
		}
	})

	t.Run("the container's own bind mounts are appended after the fixed ones, re-rooted under /cntrRootfs", func(t *testing.T) {
		s := sharedfsRootfs{
			mountedPath: "/mnt/rootfs", sfsType: "9pfs",
			mounts: []specs.Mount{{Type: "bind", Source: "/host/data", Destination: "/data"}},
		}
		mounts, err := s.getMounts()
		require.NoError(t, err)
		require.Len(t, mounts, 3)
		assert.Equal(t, containerRootfsMountPath+"/data", mounts[2].Destination)
	})
}

func TestSharedfsRootfsGetSharedDirs(t *testing.T) {
	s := sharedfsRootfs{sfsType: "virtiofs"}
	got, err := s.getSharedDirs()
	assert.NoError(t, err)
	assert.Equal(t, types.SharedfsParams{Path: containerRootfsMountPath, Type: "virtiofs"}, got)
}

func TestSharedfsRootfsPreStartCmd(t *testing.T) {
	t.Run("9pfs needs no separate process, returns nil", func(t *testing.T) {
		// Kills a mutant that drops the sfsType == "9pfs" early return --
		// 9pfs has no userspace daemon to spawn, unlike virtiofsd.
		s := sharedfsRootfs{sfsType: "9pfs"}
		assert.Nil(t, s.preStartCmd())
	})

	t.Run("virtiofs returns the virtiofsd argv with the binary as argv[0]", func(t *testing.T) {
		s := sharedfsRootfs{
			sfsType: "virtiofs", sharedPath: "/mnt/rootfs",
			vfsdConfig: types.ExtraBinConfig{Path: "/usr/libexec/virtiofsd"},
		}
		got := s.preStartCmd()
		want := []string{"/usr/libexec/virtiofsd", "--socket-path=/tmp/vhostqemu", "--shared-dir", "/mnt/rootfs"}
		assert.Equal(t, want, got)
	})

	t.Run("configured virtiofsd options are appended, space-split", func(t *testing.T) {
		// Kills a mutant that drops the Options != "" guard or the
		// strings.Fields split.
		s := sharedfsRootfs{
			sfsType: "virtiofs", sharedPath: "/mnt/rootfs",
			vfsdConfig: types.ExtraBinConfig{Path: "/usr/libexec/virtiofsd", Options: "--cache always"},
		}
		got := s.preStartCmd()
		want := []string{"/usr/libexec/virtiofsd", "--socket-path=/tmp/vhostqemu", "--shared-dir", "/mnt/rootfs", "--cache", "always"}
		assert.Equal(t, want, got)
	})
}

func TestChooseTmpfsSize(t *testing.T) {
	cases := []struct {
		name    string
		sfsType string
		mem     uint64
		want    string
		strict  bool // only checked with the strict check "memory-limit" on
	}{
		{"9pfs ignores memory entirely, always the fixed size", "9pfs", 999 * 1024 * 1024, tmpfsSizeFor9pfsRootfs, false},
		{
			// Kills a mutant that drops the +1MiB extra, or that computes
			// against decimal MB instead of MiB.
			name:    "virtiofs is guest memory plus a fixed 1MiB, in MiB",
			sfsType: "virtiofs", mem: 256 * 1024 * 1024, want: "257m", strict: true,
		},
		{"virtiofs with zero memory still gets the 1MiB extra, not 0m", "virtiofs", 0, "1m", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.strict && !strictOn("memory-limit") {
				t.Skip("off until the strict check memory-limit is on")
			}
			assert.Equal(t, tc.want, chooseTmpfsSize(tc.sfsType, tc.mem))
		})
	}
}

func TestFilterBindMounts(t *testing.T) {
	containerRootfs := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(containerRootfs, "data"), 0o755))

	t.Run("only bind mounts survive, re-rooted under the monitor's container-rootfs mount point", func(t *testing.T) {
		// Kills a mutant that drops the m.Type != "bind" skip, which would
		// let a tmpfs/proc/etc. mount fall through with a nonsensical
		// re-rooted destination.
		got, err := filterBindMounts(containerRootfs, []specs.Mount{
			{Type: "bind", Source: "/host/data", Destination: "/data", Options: []string{"rw"}},
			{Type: "tmpfs", Destination: "/tmp"},
			{Type: "proc", Destination: "/proc"},
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "bind", got[0].Type)
		assert.Equal(t, "/host/data", got[0].Source)
		assert.Equal(t, containerRootfsMountPath+"/data", got[0].Destination)
		assert.Equal(t, []string{"rw"}, got[0].Options)
	})

	t.Run("no bind mounts at all returns an empty, non-nil-checked result", func(t *testing.T) {
		got, err := filterBindMounts(containerRootfs, []specs.Mount{{Type: "tmpfs", Destination: "/tmp"}})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("a destination that escapes the container rootfs via a symlink is re-rooted safely, not followed", func(t *testing.T) {
		// SecureJoin's whole point: exercising it for real, not just trusting
		// filepath.Join, since a naive join would let ../.. escape.
		require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(containerRootfs, "escape")))
		got, err := filterBindMounts(containerRootfs, []specs.Mount{
			{Type: "bind", Source: "/host/x", Destination: "/escape/../../../../etc/shadow"},
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.True(t, strings.HasPrefix(got[0].Destination, containerRootfsMountPath),
			"re-rooted destination %q must stay under %q", got[0].Destination, containerRootfsMountPath)
	})
}
