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

	"github.com/moby/sys/mountinfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestDetectBlockFsType(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		offset  int
		magic   []byte
		want    string
		wantErr bool
		noFile  bool
	}{
		{
			name:   "UFS2",
			size:   70000,
			offset: 66908,
			magic:  []byte{0x19, 0x01, 0x54, 0x19},
			want:   "ufs",
		},
		{
			name:   "UFS1",
			size:   10000,
			offset: 9564,
			magic:  []byte{0x54, 0x19, 0x01, 0x00},
			want:   "ufs",
		},
		{
			name:   "UFS2 at 8192 (makefs layout)",
			size:   10000,
			offset: 9564,
			magic:  []byte{0x19, 0x01, 0x54, 0x19},
			want:   "ufs",
		},
		{
			name:   "ext2",
			size:   2048,
			offset: 1080,
			magic:  []byte{0x53, 0xEF},
			want:   "ext2",
		},
		{
			name: "zeros",
			size: 70000,
		},
		{
			name: "short",
			size: 100,
		},
		{
			name:    "missing path",
			wantErr: true,
			noFile:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rootfs.img")

			if !tt.noFile {
				data := make([]byte, tt.size)
				copy(data[tt.offset:], tt.magic)
				require.NoError(t, os.WriteFile(path, data, 0o600))
			}

			got, err := detectBlockFsType(path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
