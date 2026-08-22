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
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// WARNING: These tests mutate global package-level hook variables (spawnProcessHook).
// Therefore, they MUST NOT run in parallel with other tests. Do NOT add t.Parallel() to these tests.

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

func TestAdjustPathsForSharedfs(t *testing.T) {
	assert.Equal(t, "", adjustPathsForSharedfs(""))
	assert.Equal(t, containerRootfsMountPath+"/foo", adjustPathsForSharedfs("foo"))
	assert.Equal(t, containerRootfsMountPath+"/foo/bar", adjustPathsForSharedfs("/foo/bar"))
}

func TestSharedfsPreStart(t *testing.T) {
	origSpawn := spawnProcessHook
	t.Cleanup(func() { spawnProcessHook = origSpawn })

	t.Run("9pfs does nothing", func(t *testing.T) {
		spawnCalled := false
		spawnProcessHook = func(bin string, args []string) error {
			spawnCalled = true
			return nil
		}
		s := sharedfsRootfs{sfsType: "9pfs"}
		err := s.preStart()
		assert.NoError(t, err)
		assert.False(t, spawnCalled)
	})

	t.Run("virtiofs launches daemon", func(t *testing.T) {
		var spawnBin string
		var spawnArgs []string
		spawnProcessHook = func(bin string, args []string) error {
			spawnBin = bin
			spawnArgs = args
			return nil
		}
		s := sharedfsRootfs{
			sfsType:    "virtiofs",
			sharedPath: "/shared/dir",
			vfsdConfig: types.ExtraBinConfig{
				Path:    "/usr/bin/virtiofsd",
				Options: "--sandbox chroot --syslog",
			},
		}

		err := s.preStart()
		assert.NoError(t, err)
		assert.Equal(t, "/usr/bin/virtiofsd", spawnBin)
		assert.Contains(t, spawnArgs, "--socket-path=/tmp/vhostqemu")
		assert.Contains(t, spawnArgs, "--shared-dir")
		assert.Contains(t, spawnArgs, "/shared/dir")
		assert.Contains(t, spawnArgs, "--sandbox")
		assert.Contains(t, spawnArgs, "chroot")
		assert.Contains(t, spawnArgs, "--syslog")
	})

	t.Run("virtiofs fails daemon launch", func(t *testing.T) {
		spawnProcessHook = func(bin string, args []string) error {
			return errors.New("exec error")
		}
		s := sharedfsRootfs{
			sfsType:    "virtiofs",
			sharedPath: "/shared/dir",
			vfsdConfig: types.ExtraBinConfig{
				Path: "/usr/bin/virtiofsd",
			},
		}

		err := s.preStart()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to start virtiofsd")
	})
}

func TestSharedfsGetMounts(t *testing.T) {
	t.Run("virtiofs mounts", func(t *testing.T) {
		s := sharedfsRootfs{
			sfsType:     "virtiofs",
			mountedPath: "/mnt/shared",
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
		// 1. Rootfs bind mount: /mnt/shared -> containerRootfsMountPath
		// 2. Virtiofsd binary bind mount: /usr/bin/virtiofsd -> /usr/bin/virtiofsd
		// 3. /tmp tmpfs mount: /tmp (size=513m)
		// 4. Filtered bind mount: /host/a -> containerRootfsMountPath/container/a
		assert.Len(t, mounts, 4)

		assert.Equal(t, "bind", mounts[0].Type)
		assert.Equal(t, "/mnt/shared", mounts[0].Source)
		assert.Equal(t, containerRootfsMountPath, mounts[0].Destination)

		assert.Equal(t, "bind", mounts[1].Type)
		assert.Equal(t, "/usr/bin/virtiofsd", mounts[1].Source)
		assert.Equal(t, "/usr/bin/virtiofsd", mounts[1].Destination)

		assert.Equal(t, "tmpfs", mounts[2].Type)
		assert.Equal(t, "/tmp", mounts[2].Destination)
		assert.Contains(t, mounts[2].Options, "size=537m")

		assert.Equal(t, "bind", mounts[3].Type)
		assert.Equal(t, "/host/a", mounts[3].Source)
		assert.Equal(t, containerRootfsMountPath+"/container/a", mounts[3].Destination)
	})

	t.Run("9pfs mounts", func(t *testing.T) {
		s := sharedfsRootfs{
			sfsType:     "9pfs",
			mountedPath: "/mnt/shared",
			memory:      512 * 1024 * 1024,
		}

		mounts, err := s.getMounts()
		assert.NoError(t, err)
		// Expected mounts:
		// 1. Rootfs bind mount: /mnt/shared -> containerRootfsMountPath
		// 2. /tmp tmpfs mount: /tmp (size=65536k)
		assert.Len(t, mounts, 2)
		assert.Equal(t, "bind", mounts[0].Type)
		assert.Equal(t, "/mnt/shared", mounts[0].Source)
		assert.Equal(t, containerRootfsMountPath, mounts[0].Destination)

		assert.Equal(t, "tmpfs", mounts[1].Type)
		assert.Equal(t, "/tmp", mounts[1].Destination)
		assert.Contains(t, mounts[1].Options, "size=65536k")
	})
}
