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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestCheckpointMetadataRoundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	metadata := CheckpointMetadata{
		Version:       1,
		VmmType:       "firecracker",
		UnikernelType: "linux",
		Net: types.NetDevParams{
			IP:      "10.4.0.5",
			Mask:    "255.255.255.0",
			Gateway: "10.4.0.1",
			MAC:     "aa:bb:cc:dd:ee:ff",
			TapDev:  "tap0_urunc",
			MTU:     1500,
		},
		SnapshotFiles: []string{"vmstate", "memory"},
	}

	require.NoError(t, writeCheckpointMetadata(dir, metadata))
	got, err := readCheckpointMetadata(dir)
	require.NoError(t, err)
	assert.Equal(t, metadata, got)
}

func TestReadCheckpointMetadataMissing(t *testing.T) {
	t.Parallel()
	_, err := readCheckpointMetadata(t.TempDir())
	assert.Error(t, err)
}

func TestCopyAndMoveDirContents(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dst := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "vmstate"), []byte("state"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(src, "memory"), []byte("mem"), 0o600))
	// Non-regular entries (directories) are skipped.
	require.NoError(t, os.Mkdir(filepath.Join(src, "subdir"), 0o700))

	names, err := copyDirContents(src, dst)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"vmstate", "memory"}, names)
	data, err := os.ReadFile(filepath.Join(dst, "vmstate"))
	require.NoError(t, err)
	assert.Equal(t, "state", string(data))
	// Source files remain after a copy.
	_, err = os.Stat(filepath.Join(src, "vmstate"))
	assert.NoError(t, err)

	dst2 := t.TempDir()
	names, err = moveDirContents(src, dst2)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"vmstate", "memory"}, names)
	// Source files are removed after a move.
	_, err = os.Stat(filepath.Join(src, "vmstate"))
	assert.True(t, os.IsNotExist(err))
}

func TestNetInfoRoundtrip(t *testing.T) {
	t.Parallel()
	u := &Unikontainer{BaseDir: t.TempDir()}
	netArgs := types.NetDevParams{
		IP:     "10.4.0.5",
		MAC:    "aa:bb:cc:dd:ee:ff",
		TapDev: "tap2_urunc",
		MTU:    1500,
	}
	require.NoError(t, u.saveNetInfo(netArgs))
	got, err := u.loadNetInfo()
	require.NoError(t, err)
	assert.Equal(t, netArgs, got)
}
