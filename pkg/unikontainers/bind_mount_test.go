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

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestIsBindMount(t *testing.T) {
	t.Parallel()

	// Docker spells a volume as type bind with an rbind option.
	assert.True(t, isBindMount(specs.Mount{Type: "bind", Options: []string{"rbind", "ro"}}))
	// nerdctl and containerd spell a volume as type none with an rbind option.
	assert.True(t, isBindMount(specs.Mount{Type: "none", Options: []string{"rbind", "rprivate", "rw"}}))
	assert.True(t, isBindMount(specs.Mount{Options: []string{"bind"}}))
	// The options alone decide: a bind or rbind Type without such an option is
	// not a bind mount.
	assert.False(t, isBindMount(specs.Mount{Type: "bind"}))
	assert.False(t, isBindMount(specs.Mount{Type: "rbind", Options: []string{"rw"}}))
	assert.False(t, isBindMount(specs.Mount{Type: "tmpfs", Options: []string{"nosuid"}}))
	assert.False(t, isBindMount(specs.Mount{Type: "proc"}))

	// rebaseBindMounts re-roots such volumes under the shared rootfs.
	dir := t.TempDir()
	vol := t.TempDir()
	mounts, err := rebaseBindMounts(dir, []specs.Mount{
		{Type: "none", Source: vol, Destination: "/data", Options: []string{"rbind", "rw"}},
	})
	require.NoError(t, err)
	require.Len(t, mounts, 1)
	assert.Equal(t, vol, mounts[0].Source)
	assert.Equal(t, containerRootfsMountPath+"/data", mounts[0].Destination)
}

func TestOnlyBindMounts(t *testing.T) {
	t.Parallel()

	vol := t.TempDir()
	file := filepath.Join(vol, "file")
	require.NoError(t, os.WriteFile(file, []byte("data"), 0o600))

	mounts := []specs.Mount{
		{Type: "bind", Source: vol, Destination: "/data", Options: []string{"rbind"}},
		{Type: "none", Source: file, Destination: "/etc/file", Options: []string{"bind", "ro"}},
		{Type: "tmpfs", Source: "tmpfs", Destination: "/tmp"},
		{Type: "sysfs", Source: "sysfs", Destination: "/sys"},
		// Rootless runtimes emit an rbind of the host's /sys in place of sysfs.
		{Type: "none", Source: "/sys", Destination: "/sys", Options: []string{"rbind", "nosuid", "noexec", "nodev", "ro"}},
		{Type: "bind", Source: "/proc", Destination: "/host/proc", Options: []string{"rbind"}},
		{Type: "bind", Source: "/proc/cpuinfo", Destination: "/cpuinfo", Options: []string{"bind"}},
	}
	// Kernel filesystems mounted on their own, e.g. cgroupfs under /sys, when
	// this host has them.
	for _, kfs := range []struct {
		path  string
		magic int64
	}{
		{"/sys/fs/cgroup", unix.CGROUP2_SUPER_MAGIC},
		{"/sys/kernel/config", configfsMagic},
		{"/sys/fs/fuse/connections", fusectlMagic},
		{"/dev/mqueue", mqueueMagic},
		{"/sys/fs/selinux", unix.SELINUX_MAGIC},
	} {
		var st unix.Statfs_t
		if unix.Statfs(kfs.path, &st) == nil && int64(st.Type) == kfs.magic {
			mounts = append(mounts, specs.Mount{Type: "bind", Source: kfs.path, Destination: "/host" + kfs.path, Options: []string{"rbind"}})
		}
	}

	result, err := onlyBindMounts(mounts)
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, vol, result[0].Source)
	assert.Equal(t, file, result[1].Source)

	_, err = onlyBindMounts([]specs.Mount{
		{Type: "bind", Source: filepath.Join(vol, "missing"), Destination: "/missing", Options: []string{"rbind"}},
	})
	assert.Error(t, err)

	// A bind or rbind Type without such an option, as `ctr run --mount
	// type=bind,...` emits, is an error rather than a silently dropped volume.
	for _, typ := range []string{"bind", "rbind"} {
		_, err = onlyBindMounts([]specs.Mount{
			{Type: typ, Source: vol, Destination: "/data", Options: []string{"rw"}},
		})
		assert.ErrorContains(t, err, "no bind or rbind option")
	}
}

func TestCopyMountfilesSkipsSpecialFiles(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	file := filepath.Join(src, "file")
	require.NoError(t, os.WriteFile(file, []byte("data"), 0o600))
	fifo := filepath.Join(src, "fifo")
	require.NoError(t, unix.Mkfifo(fifo, 0o600))

	assert.ErrorIs(t, fileFromHost(t.TempDir(), fifo, "/fifo"), ErrNotRegularFile)
	assert.ErrorIs(t, fileFromHost(t.TempDir(), "/dev/null", "/dev/null"), ErrNotRegularFile)
	assert.ErrorIs(t, fileFromHost(t.TempDir(), src, "/data"), ErrNotRegularFile)

	target := t.TempDir()
	require.NoError(t, copyMountfiles(target, []specs.Mount{
		{Type: "none", Source: file, Destination: "/etc/file", Options: []string{"rbind"}},
		{Type: "none", Source: fifo, Destination: "/run/fifo", Options: []string{"rbind"}},
		{Type: "bind", Source: "/dev/null", Destination: "/dev/null", Options: []string{"rbind"}},
		{Type: "none", Source: src, Destination: "/data", Options: []string{"rbind"}},
	}))

	content, err := os.ReadFile(filepath.Join(target, "etc/file"))
	require.NoError(t, err)
	assert.Equal(t, "data", string(content))
	assert.NoFileExists(t, filepath.Join(target, "run/fifo"))
	assert.NoFileExists(t, filepath.Join(target, "dev/null"))
}
