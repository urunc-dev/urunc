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
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestChooseTmpfsSize(t *testing.T) {
	tests := []struct {
		name     string
		sfsType  string
		mem      uint64
		expected string
	}{
		{"9pfs size", "9pfs", 1024 * 1024, tmpfsSizeFor9pfsRootfs},
		{"virtiofs 0 mem", "virtiofs", 0, "1m"},
		{"virtiofs 1024MB", "virtiofs", 1024 * 1024 * 1024, "1074m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, chooseTmpfsSize(tt.sfsType, tt.mem))
		})
	}
}

func TestSharedfsPreStartCmd(t *testing.T) {
	t.Run("9pfs returns nil", func(t *testing.T) {
		s := sharedfsRootfs{sfsType: "9pfs"}
		assert.Nil(t, s.preStartCmd())
	})

	t.Run("virtiofs returns daemon command and arguments", func(t *testing.T) {
		s := sharedfsRootfs{
			sfsType:    "virtiofs",
			sharedPath: "/shared/dir",
			vfsdConfig: types.ExtraBinConfig{
				Path:    "/usr/bin/virtiofsd",
				Options: "--sandbox chroot --syslog",
			},
		}

		expected := []string{
			"/usr/bin/virtiofsd",
			"--socket-path=/tmp/vhostqemu",
			"--shared-dir",
			"/shared/dir",
			"--sandbox",
			"chroot",
			"--syslog",
		}
		assert.Equal(t, expected, s.preStartCmd())
	})

	t.Run("virtiofs without options", func(t *testing.T) {
		s := sharedfsRootfs{
			sfsType:    "virtiofs",
			sharedPath: "/shared/dir",
			vfsdConfig: types.ExtraBinConfig{
				Path: "/usr/bin/virtiofsd",
			},
		}

		expected := []string{
			"/usr/bin/virtiofsd",
			"--socket-path=/tmp/vhostqemu",
			"--shared-dir",
			"/shared/dir",
		}
		assert.Equal(t, expected, s.preStartCmd())
	})
}

func TestSharedfsGetMounts(t *testing.T) {
	t.Run("virtiofs mounts", func(t *testing.T) {
		tmpDir := t.TempDir()
		s := sharedfsRootfs{
			sfsType:     "virtiofs",
			mountedPath: tmpDir,
			vfsdConfig: types.ExtraBinConfig{
				Path: "/usr/bin/virtiofsd",
			},
			memory: 512 * 1024 * 1024,
			mounts: []specs.Mount{
				{Type: "bind", Source: "/host/a", Destination: "/container/a"},
				{Type: "proc", Source: "proc", Destination: "/proc"}, // should be filtered out
			},
		}

		mounts, err := s.getMounts()
		assert.NoError(t, err)
		// Expected mounts:
		// 1. Rootfs bind mount: mountedPath -> containerRootfsMountPath (nodev, nosuid, noexec)
		// 2. Virtiofsd binary bind mount: /usr/bin/virtiofsd -> /usr/bin/virtiofsd (ro)
		// 3. /tmp tmpfs mount: /tmp (size=537m)
		// 4. Filtered bind mount: /host/a -> containerRootfsMountPath/container/a
		assert.Len(t, mounts, 4)

		assert.Equal(t, "bind", mounts[0].Type)
		assert.Equal(t, tmpDir, mounts[0].Source)
		assert.Equal(t, containerRootfsMountPath, mounts[0].Destination)
		assert.Equal(t, []string{"bind", "private", "nodev", "nosuid", "noexec"}, mounts[0].Options)

		assert.Equal(t, "bind", mounts[1].Type)
		assert.Equal(t, "/usr/bin/virtiofsd", mounts[1].Source)
		assert.Equal(t, "/usr/bin/virtiofsd", mounts[1].Destination)
		assert.Equal(t, []string{"bind", "private", "ro"}, mounts[1].Options)

		assert.Equal(t, "tmpfs", mounts[2].Type)
		assert.Equal(t, "/tmp", mounts[2].Destination)
		assert.Contains(t, mounts[2].Options, "size=537m")

		assert.Equal(t, "bind", mounts[3].Type)
		assert.Equal(t, "/host/a", mounts[3].Source)
		assert.Equal(t, filepath.Join(containerRootfsMountPath, "container/a"), mounts[3].Destination)
	})

	t.Run("9pfs mounts", func(t *testing.T) {
		tmpDir := t.TempDir()
		s := sharedfsRootfs{
			sfsType:     "9pfs",
			mountedPath: tmpDir,
			memory:      512 * 1024 * 1024,
		}

		mounts, err := s.getMounts()
		assert.NoError(t, err)
		// Expected mounts:
		// 1. Rootfs bind mount: mountedPath -> containerRootfsMountPath (nodev, nosuid, noexec)
		// 2. /tmp tmpfs mount: /tmp (size=65536k)
		assert.Len(t, mounts, 2)
		assert.Equal(t, "bind", mounts[0].Type)
		assert.Equal(t, tmpDir, mounts[0].Source)
		assert.Equal(t, containerRootfsMountPath, mounts[0].Destination)
		assert.Equal(t, []string{"bind", "private", "nodev", "nosuid", "noexec"}, mounts[0].Options)

		assert.Equal(t, "tmpfs", mounts[1].Type)
		assert.Equal(t, "/tmp", mounts[1].Destination)
		assert.Contains(t, mounts[1].Options, "size=65536k")
	})
}
