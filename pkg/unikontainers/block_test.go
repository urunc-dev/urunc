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

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestRollbackBlockVolumesNoVolumes(t *testing.T) {
	// With no already-unmounted volumes to restore, rollbackBlockVolumes
	// must return the original error unchanged.
	origErr := errors.New("original failure")

	err := rollbackBlockVolumes(nil, origErr)
	assert.ErrorIs(t, err, origErr, "Expected the original error to be returned unchanged")

	err = rollbackBlockVolumes([]types.BlockDevParams{}, origErr)
	assert.ErrorIs(t, err, origErr, "Expected the original error to be returned unchanged")
}

func TestRollbackBlockVolumesSkipsNonHostMounts(t *testing.T) {
	// Volumes without a HostMountPoint were never unmounted by
	// getBlockVolumes (e.g. an explicit block image), so
	// restoreBlockVolumes must skip them and rollback must still
	// surface the original error.
	origErr := errors.New("original failure")
	blkImgs := []types.BlockDevParams{
		{Source: "/dev/loop0", MountPoint: "/mnt/vol0"},
	}

	err := rollbackBlockVolumes(blkImgs, origErr)
	assert.ErrorIs(t, err, origErr, "Expected the original error to be returned unchanged")
}

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
