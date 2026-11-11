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

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareXDGRuntimeDir(t *testing.T) {
	t.Parallel()
	xdg := t.TempDir()

	root, err := prepareXDGRuntimeDir(xdg)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(xdg, "urunc"), root)

	fi, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
	assert.Equal(t, os.FileMode(0o700)|os.ModeSticky, fi.Mode()&(os.ModePerm|os.ModeSticky))

	_, err = prepareXDGRuntimeDir(xdg)
	require.NoError(t, err, "an existing directory is reused")
}

func TestPrepareXDGRuntimeDirMissing(t *testing.T) {
	t.Parallel()
	_, err := prepareXDGRuntimeDir(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)
}

func TestPrepareXDGRuntimeDirStaysInside(t *testing.T) {
	t.Parallel()
	xdg := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Chmod(outside, 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(xdg, "urunc")))

	_, err := prepareXDGRuntimeDir(xdg)
	require.Error(t, err)

	fi, err := os.Stat(outside)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), fi.Mode()&(os.ModePerm|os.ModeSticky), "the directory outside is not changed")
}
