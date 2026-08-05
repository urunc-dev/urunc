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
	"testing"

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

func TestHandleExplicitBlockImages(t *testing.T) {
	t.Run("parses an ordered list of block images and mount points", func(t *testing.T) {
		blocks, err := handleExplicitBlockImages("/.boot/disk1, /.boot/disk2", "/data1, /data2")
		assert.NoError(t, err)
		assert.Len(t, blocks, 2)
		assert.Equal(t, "/.boot/disk1", blocks[0].Source)
		assert.Equal(t, "/data1", blocks[0].MountPoint)
		assert.Equal(t, "/.boot/disk2", blocks[1].Source)
		assert.Equal(t, "/data2", blocks[1].MountPoint)
	})

	t.Run("single image stays backward compatible", func(t *testing.T) {
		blocks, err := handleExplicitBlockImages("/.boot/rootfs", "/")
		assert.NoError(t, err)
		assert.Len(t, blocks, 1)
		assert.Equal(t, "rootfs", blocks[0].ID)
	})

	t.Run("empty block image yields no devices", func(t *testing.T) {
		blocks, err := handleExplicitBlockImages("", "")
		assert.NoError(t, err)
		assert.Empty(t, blocks)
	})

	t.Run("mismatched counts return an error", func(t *testing.T) {
		_, err := handleExplicitBlockImages("/a,/b", "/data")
		assert.Error(t, err)
	})

	t.Run("rootfs combined with other block devices returns an error", func(t *testing.T) {
		_, err := handleExplicitBlockImages("/.boot/rootfs,/.boot/data", "/,/data")
		assert.Error(t, err)
	})

	t.Run("empty entries return an error", func(t *testing.T) {
		_, err := handleExplicitBlockImages("/a,,/c", "/1,/2,/3")
		assert.Error(t, err)

		_, err = handleExplicitBlockImages("/a,/b", "/1,")
		assert.Error(t, err)
	})
}

func TestNoRootfsMultipleBlockDevs(t *testing.T) {
	n := noRootfs{
		annotBlockPath:       "/.boot/disk1,/.boot/disk2",
		annotBlockMountPoint: "/data1,/data2",
	}
	blocks, err := n.getBlockDevs()
	assert.NoError(t, err)
	assert.Len(t, blocks, 2)
	assert.Equal(t, "/.boot/disk1", blocks[0].Source)
	assert.Equal(t, "/.boot/disk2", blocks[1].Source)
	assert.NotEqual(t, blocks[0].ID, blocks[1].ID, "multiple block devices must have unique IDs")
}
