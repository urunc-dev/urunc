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
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestGetBlockDevice(t *testing.T) {
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

func TestGetMountInfo_NotAMountpoint(t *testing.T) {
	notAMountpoint := t.TempDir()

	_, err := getMountInfo(notAMountpoint)
	assert.ErrorIs(t, err, ErrMountpoint, "expected ErrMountpoint for a non-mountpoint path")
}

func TestGetMountInfo_EmptyPath(t *testing.T) {
	_, err := getMountInfo("")
	assert.Error(t, err, "expected an error for empty path")
}

func FuzzGetMountInfo(f *testing.F) {
	seeds := []string{
		"/proc",
		"/",
		"/nonexistent/path",
		"",
		"////",
		"../../../../etc/passwd",
		"/proc/self/mountinfo",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, path string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("getMountInfo(%q) panicked: %v", path, r)
			}
		}()

		bd, err := getMountInfo(path)
		if err != nil {
			assert.Equal(t, types.BlockDevParams{}, bd, "on error, BlockDevParams should be zero value")
			return
		}
		assert.Equal(t, path, bd.MountPoint, "MountPoint in result should match queried path")
		assert.NotEmpty(t, bd.Source, "Source should not be empty on success")
	})
}

func TestHandleExplicitBlockImage(t *testing.T) {
	tests := []struct {
		name        string
		blockImg    string
		mountPoint  string
		wantSource  string
		wantMount   string
		wantID      string
		wantErr     bool
		errContains string
	}{
		{
			name:       "no block image requested",
			blockImg:   "",
			mountPoint: "",
			wantSource: "",
			wantMount:  "",
			wantID:     "",
			wantErr:    false,
		},
		{
			name:       "no block image requested, mountpoint ignored",
			blockImg:   "",
			mountPoint: "/data",
			wantSource: "",
			wantMount:  "",
			wantID:     "",
			wantErr:    false,
		},
		{
			name:        "block image without mountpoint is an error",
			blockImg:    "/dev/sdb1",
			mountPoint:  "",
			wantErr:     true,
			errContains: "annotation for block device was set without a mountpoint",
		},
		{
			name:       "block image mounted at root gets rootfs ID",
			blockImg:   "/dev/sdb1",
			mountPoint: "/",
			wantSource: "/dev/sdb1",
			wantMount:  "/",
			wantID:     "rootfs",
			wantErr:    false,
		},
		{
			name:       "block image mounted elsewhere has empty ID",
			blockImg:   "/dev/sdb1",
			mountPoint: "/data",
			wantSource: "/dev/sdb1",
			wantMount:  "/data",
			wantID:     "",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handleExplicitBlockImage(tt.blockImg, tt.mountPoint)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantSource, got.Source)
			assert.Equal(t, tt.wantMount, got.MountPoint)
			assert.Equal(t, tt.wantID, got.ID)
		})
	}
}

func FuzzHandleExplicitBlockImage(f *testing.F) {
	seeds := []struct {
		blockImg   string
		mountPoint string
	}{
		{"", ""},
		{"/dev/sdb1", "/"},
		{"/dev/sdb1", ""},
		{"/dev/sdb1", "/data"},
		{"", "/data"},
		{"img", "/"},
	}
	for _, s := range seeds {
		f.Add(s.blockImg, s.mountPoint)
	}

	f.Fuzz(func(t *testing.T, blockImg string, mountPoint string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("handleExplicitBlockImage(%q, %q) panicked: %v", blockImg, mountPoint, r)
			}
		}()

		got, err := handleExplicitBlockImage(blockImg, mountPoint)

		switch {
		case blockImg == "":
			assert.NoError(t, err)
			assert.Equal(t, types.BlockDevParams{}, got)

		case mountPoint == "":
			assert.Error(t, err)
			assert.Equal(t, types.BlockDevParams{}, got)

		case mountPoint == "/":
			assert.NoError(t, err)
			assert.Equal(t, blockImg, got.Source)
			assert.Equal(t, "/", got.MountPoint)
			assert.Equal(t, "rootfs", got.ID)

		default:
			assert.NoError(t, err)
			assert.Equal(t, blockImg, got.Source)
			assert.Equal(t, mountPoint, got.MountPoint)
			assert.Empty(t, got.ID)
		}
	})
}

func TestBlockDevNodes_NonBlockRootfs(t *testing.T) {
	rootfs := types.RootfsParams{Type: "shared"}

	devs, err := blockDevNodes([]types.BlockDevParams{{Source: "/dev/sdb1"}}, rootfs)
	assert.NoError(t, err)
	assert.Nil(t, devs, "non-block rootfs should never produce device nodes")
}

func TestBlockDevNodes_EmptyBlockArgs(t *testing.T) {
	rootfs := types.RootfsParams{Type: "block"}

	devs, err := blockDevNodes(nil, rootfs)
	assert.NoError(t, err)
	assert.Nil(t, devs)
}

func TestBlockDevNodes_SkipsRootfsDeviceWhenNotConverted(t *testing.T) {
	rootfs := types.RootfsParams{
		Type:        "block",
		Path:        "/dev/sdb1",
		MountedPath: "",
	}
	blockArgs := []types.BlockDevParams{
		{Source: "/dev/sdb1"},
	}

	devs, err := blockDevNodes(blockArgs, rootfs)
	assert.NoError(t, err)
	assert.Empty(t, devs, "the rootfs device itself should be skipped when MountedPath is empty")
}

func TestCopyMountfiles_SkipsNonBindMounts(t *testing.T) {
	target := t.TempDir()
	mounts := []specs.Mount{
		{Type: "tmpfs", Source: "tmpfs", Destination: "/tmp"},
		{Type: "proc", Source: "proc", Destination: "/proc"},
	}

	err := copyMountfiles(target, mounts)
	assert.NoError(t, err)
}

func TestCopyMountfiles_NoMounts(t *testing.T) {
	err := copyMountfiles(t.TempDir(), nil)
	assert.NoError(t, err)
}

func TestCopyMountfiles_BindMountMissingSource(t *testing.T) {
	target := t.TempDir()
	mounts := []specs.Mount{
		{Type: "bind", Source: "/definitely/does/not/exist", Destination: "/data"},
	}

	err := copyMountfiles(target, mounts)
	assert.Error(t, err, "expected an error for a bind mount whose source doesn't exist")
}

func TestExtractBootFiles_MissingUnikernel(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()

	err := extractBootFiles(oldRoot, newRoot, "unikernel-bin", "urunc.json", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "could not move")
}

func TestBlockRootfs_PreSetup_NoMountedPath(t *testing.T) {
	b := blockRootfs{mountedPath: ""}

	err := b.preSetup()
	assert.NoError(t, err, "preSetup should be a no-op when mountedPath is empty")
}

func TestBlockRootfs_PostSetup(t *testing.T) {
	b := blockRootfs{}
	assert.NoError(t, b.postSetup())
}

func TestBlockRootfs_PreStart(t *testing.T) {
	b := blockRootfs{}
	assert.NoError(t, b.preStart())
}

func TestBlockRootfs_GetSharedDirs(t *testing.T) {
	b := blockRootfs{}

	dirs, err := b.getSharedDirs()
	assert.NoError(t, err)
	assert.Equal(t, types.SharedfsParams{}, dirs)
}

func TestBlockRootfs_GetMounts(t *testing.T) {
	b := blockRootfs{}

	mounts, err := b.getMounts()
	assert.NoError(t, err)
	assert.Len(t, mounts, 1)
	assert.Equal(t, "/tmp", mounts[0].Destination)
}

func TestBlockRootfs_GetBlockDevs_DefaultGuestMountsAtRoot(t *testing.T) {
	b := blockRootfs{
		path:      "/dev/sdb1",
		guestType: "mirage",
	}

	devs, err := b.getBlockDevs()
	assert.NoError(t, err)
	assert.Len(t, devs, 1)
	assert.Equal(t, "/", devs[0].MountPoint)
	assert.Equal(t, "rootfs", devs[0].ID)
	assert.Equal(t, "/dev/sdb1", devs[0].Source)
}

func TestBlockRootfs_GetBlockDevs_RumprunMountsAtData(t *testing.T) {
	b := blockRootfs{
		path:      "/dev/sdb1",
		guestType: "rumprun",
	}

	devs, err := b.getBlockDevs()
	assert.NoError(t, err)
	assert.Len(t, devs, 1)
	assert.Equal(t, "/data", devs[0].MountPoint, "rumprun must not mount its rootfs block at /")
	assert.Equal(t, "rootfs", devs[0].ID)
}

func TestSetLoopAutoclear_NotALoopDevice(t *testing.T) {
	regularFile := filepath.Join(t.TempDir(), "not-a-loop-device")
	assert.NoError(t, os.WriteFile(regularFile, []byte("x"), 0644))

	changed, err := setLoopAutoclear(regularFile, true)
	assert.NoError(t, err)
	assert.False(t, changed, "expected no change reported for a non-loop-device path")
}

func FuzzSetLoopAutoclear(f *testing.F) {
	seeds := []string{
		"/dev/loop0",
		"/dev/null",
		"",
		"not-a-real-device",
		"/dev/sda",
	}
	for _, s := range seeds {
		f.Add(s, true)
		f.Add(s, false)
	}

	f.Fuzz(func(t *testing.T, devPath string, autoclear bool) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("setLoopAutoclear(%q, %v) panicked: %v", devPath, autoclear, r)
			}
		}()

		_, _ = setLoopAutoclear(devPath, autoclear)
	})
}

func TestErrMountpointIsSentinel(t *testing.T) {
	wrapped := errors.New("wrapped: " + ErrMountpoint.Error())
	assert.NotErrorIs(t, wrapped, ErrMountpoint, "a manually re-created error should not match errors.Is")

	assert.ErrorIs(t, ErrMountpoint, ErrMountpoint)
}
