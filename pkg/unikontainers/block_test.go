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
	"strings"
	"testing"

	"github.com/moby/sys/mountinfo"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestGetBlockDevice(t *testing.T) {
	// Create a mock partition
	tmpMnt := types.BlockDevParams{
		Source:     "proc",
		MountPoint: "/proc",
		FsType:     "proc",
		ID:         "",
	}

	rootFs, err := getMountInfo("/proc")
	assert.NoError(t, err, "Expected no error in getting block device")
	assert.Equal(t, tmpMnt.Source, rootFs.Source, "Incorrect image")
	assert.Equal(t, tmpMnt.MountPoint, rootFs.MountPoint, "Incorrect mountpoint")
	assert.Equal(t, tmpMnt.FsType, rootFs.FsType, "Expected filesystem type to be proc")
	assert.Equal(t, tmpMnt.ID, rootFs.ID, "Expected ID to be empty")
}

// TestFindMountInfoEscapedPath reproduces a bind mount whose source path
// contains a space, a tab or a backslash. The kernel octal-escapes these
// characters in mountinfo (e.g. a space becomes \040), so this makes sure
// that findMountInfo still matches the real, unescaped path against the
// parsed mountinfo entry.
func TestFindMountInfoEscapedPath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		escapedRaw string
	}{
		{name: "space", path: "/mnt/my volume", escapedRaw: `/mnt/my\040volume`},
		{name: "tab", path: "/mnt/my\tvolume", escapedRaw: `/mnt/my\011volume`},
		{name: "backslash", path: `/mnt/my\volume`, escapedRaw: `/mnt/my\134volume`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := "36 35 8:1 / " + tt.escapedRaw + " rw,relatime shared:1 - ext4 /dev/sdb1 rw"
			mounts, err := mountinfo.GetMountsFromReader(strings.NewReader(line), nil)
			assert.NoError(t, err, "expected the synthetic mountinfo line to parse")

			blockDev, err := findMountInfo(mounts, tt.path)
			assert.NoError(t, err, "expected the escaped mount point to match the real path")
			assert.Equal(t, "/dev/sdb1", blockDev.Source)
			assert.Equal(t, "ext4", blockDev.FsType)
			assert.Equal(t, tt.path, blockDev.MountPoint)
		})
	}
}

// stubUnikernel only implements the parts of types.Unikernel that
// blockVolumesFrom can reach in the tests below.
type stubUnikernel struct {
	types.Unikernel
	fsType string
}

func (s stubUnikernel) SupportsFS(fsType string) bool {
	return fsType == s.fsType
}

func testBinds(n int) []specs.Mount {
	mounts := make([]specs.Mount, n)
	for i := range mounts {
		mounts[i] = specs.Mount{
			Source:      fmt.Sprintf("/mnt/vol%d", i),
			Destination: fmt.Sprintf("/data/vol%d", i),
			Type:        "bind",
		}
	}
	return mounts
}

// TestBlockVolumesShareMountInfoSnapshot makes sure the mount table is read
// once for all the bind mounts instead of once per bind. None of the binds
// below is mounted, so every lookup ends in ErrMountpoint and no unmount
// (which would need a real mount) is triggered.
func TestBlockVolumesShareMountInfoSnapshot(t *testing.T) {
	reads := 0
	readMountInfo := func() ([]*mountinfo.Info, error) {
		reads++
		return nil, nil
	}

	vols, err := blockVolumesFrom(testBinds(8), stubUnikernel{}, readMountInfo)
	assert.NoError(t, err)
	assert.Empty(t, vols)
	assert.Equal(t, 1, reads, "mountinfo must be read once for all binds")
}

func BenchmarkBlockVolumesShareMountInfoSnapshot(b *testing.B) {
	line := "36 35 8:1 / /mnt/data rw,relatime shared:1 - ext4 /dev/sdb1 rw\n"
	readMountInfo := func() ([]*mountinfo.Info, error) {
		return mountinfo.GetMountsFromReader(strings.NewReader(strings.Repeat(line, 500)), nil)
	}
	mounts := testBinds(32)

	b.ResetTimer()
	for b.Loop() {
		if _, err := blockVolumesFrom(mounts, stubUnikernel{}, readMountInfo); err != nil {
			b.Fatal(err)
		}
	}
}
