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

package initrd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cavaliergopher/cpio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddFileToInitrdAppendsRecord(t *testing.T) {
	initrdPath := filepath.Join(t.TempDir(), "initrd.cpio")
	const data = "{\"cmd\":[\"/bin/sh\"]}\n"

	require.NoError(t, AddFileToInitrd(initrdPath, data, "urunit.conf"))
	require.NoError(t, AddFileToInitrd(initrdPath, "second", "other"))

	f, err := os.Open(initrdPath)
	require.NoError(t, err)
	defer f.Close()

	// Each call appends its own archive, so the reader stops at the first
	// trailer and sees the first file only.
	r := cpio.NewReader(f)
	hdr, err := r.Next()
	require.NoError(t, err)
	assert.Equal(t, "urunit.conf", hdr.Name)
	assert.Equal(t, cpio.FileMode(0o100400), hdr.Mode)
	assert.Equal(t, 0, hdr.Uid)
	assert.Equal(t, 0, hdr.Guid)
	// cpio.Writer numbers a record that asks for inode 0, starting at 1.
	assert.Equal(t, int64(1), hdr.Inode)
	assert.Equal(t, int64(len(data)), hdr.Size)
	assert.False(t, hdr.ModTime.IsZero())

	content, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, data, string(content))

	_, err = r.Next()
	assert.True(t, errors.Is(err, io.EOF), "got %v", err)
}

func TestAddFileToInitrdMissingDir(t *testing.T) {
	initrdPath := filepath.Join(t.TempDir(), "missing", "initrd.cpio")
	assert.Error(t, AddFileToInitrd(initrdPath, "x", "x"))
}
