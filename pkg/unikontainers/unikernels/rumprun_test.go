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

package unikernels

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestRumprunMonitorBlockCli(t *testing.T) {
	t.Run("returns nil when no block devices exist", func(t *testing.T) {
		r := &Rumprun{}
		err := r.Init(types.UnikernelParams{
			Monitor: "hvt",
			Block:   []types.BlockDevParams{},
		})
		assert.NoError(t, err)
		blkArgs := r.MonitorBlockCli()
		assert.Nil(t, blkArgs)
	})

	t.Run("returns single block device with default ID rootfs", func(t *testing.T) {
		r := &Rumprun{}
		err := r.Init(types.UnikernelParams{
			Monitor: "hvt",
			Block: []types.BlockDevParams{
				{
					Source:     "/var/lib/urunc/rootfs.img",
					MountPoint: "/data",
				},
			},
		})
		assert.NoError(t, err)
		blkArgs := r.MonitorBlockCli()
		assert.Len(t, blkArgs, 1)
		assert.Equal(t, "rootfs", blkArgs[0].ID)
		assert.Equal(t, "/var/lib/urunc/rootfs.img", blkArgs[0].Path)
	})

	t.Run("returns multiple block devices with generated IDs", func(t *testing.T) {
		r := &Rumprun{}
		err := r.Init(types.UnikernelParams{
			Monitor: "spt",
			Block: []types.BlockDevParams{
				{
					Source:     "/var/lib/urunc/rootfs.img",
					MountPoint: "/data",
				},
				{
					Source:     "/var/lib/urunc/vol1.img",
					MountPoint: "/mnt/vol1",
				},
				{
					Source:     "/var/lib/urunc/vol2.img",
					MountPoint: "/mnt/vol2",
				},
			},
		})
		assert.NoError(t, err)
		blkArgs := r.MonitorBlockCli()
		assert.Len(t, blkArgs, 3)
		assert.Equal(t, "rootfs", blkArgs[0].ID)
		assert.Equal(t, "/var/lib/urunc/rootfs.img", blkArgs[0].Path)
		assert.Equal(t, "vol1", blkArgs[1].ID)
		assert.Equal(t, "/var/lib/urunc/vol1.img", blkArgs[1].Path)
		assert.Equal(t, "vol2", blkArgs[2].ID)
		assert.Equal(t, "/var/lib/urunc/vol2.img", blkArgs[2].Path)
	})

	t.Run("preserves explicit block IDs", func(t *testing.T) {
		r := &Rumprun{}
		err := r.Init(types.UnikernelParams{
			Monitor: "hvt",
			Block: []types.BlockDevParams{
				{
					ID:         "custom_root",
					Source:     "/path/to/root.img",
					MountPoint: "/data",
				},
				{
					ID:         "db_storage",
					Source:     "/path/to/db.img",
					MountPoint: "/db",
				},
			},
		})
		assert.NoError(t, err)
		blkArgs := r.MonitorBlockCli()
		assert.Len(t, blkArgs, 2)
		assert.Equal(t, "custom_root", blkArgs[0].ID)
		assert.Equal(t, "/path/to/root.img", blkArgs[0].Path)
		assert.Equal(t, "db_storage", blkArgs[1].ID)
		assert.Equal(t, "/path/to/db.img", blkArgs[1].Path)
	})

	t.Run("returns nil for non-Solo5 monitors", func(t *testing.T) {
		r := &Rumprun{}
		err := r.Init(types.UnikernelParams{
			Monitor: "qemu",
			Block: []types.BlockDevParams{
				{
					Source:     "/path/to/root.img",
					MountPoint: "/data",
				},
			},
		})
		assert.NoError(t, err)
		blkArgs := r.MonitorBlockCli()
		assert.Nil(t, blkArgs)
	})
}
