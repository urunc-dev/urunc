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

	"github.com/containerd/containerd/mount"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/unix"
)

// WARNING: These tests mutate global package-level hook variables (mkdirAllHook, statSyscall, etc.).
// Therefore, they MUST NOT run in parallel with other tests. Do NOT add t.Parallel() to these tests.

type mountCall struct {
	source string
	target string
	fstype string
	flags  uintptr
	data   string
}

func TestSetupDev(t *testing.T) {
	origRunningInUserNS := runningInUserNSHook
	origSecureJoin := secureJoinHook
	origContainerdMount := containerdMountHook
	origStat := statSyscall
	origMkdirAll := mkdirAllHook
	origMknod := mknodSyscall
	origChmod := chmodSyscall
	origChown := chownSyscall
	t.Cleanup(func() {
		runningInUserNSHook = origRunningInUserNS
		secureJoinHook = origSecureJoin
		containerdMountHook = origContainerdMount
		statSyscall = origStat
		mkdirAllHook = origMkdirAll
		mknodSyscall = origMknod
		chmodSyscall = origChmod
		chownSyscall = origChown
	})

	t.Run("running in user namespace", func(t *testing.T) {
		runningInUserNSHook = func() bool { return true }
		var mounts []mountCall
		secureJoinHook = func(root, path string) (string, error) {
			return filepath.Join(root, path), nil
		}
		containerdMountHook = func(m *mount.Mount, target string) error {
			mounts = append(mounts, mountCall{m.Source, target, m.Type, 0, ""})
			return nil
		}
		mkdirAllHook = func(path string, perm os.FileMode) error { return nil }
		statSyscall = func(path string, stat *unix.Stat_t) error {
			stat.Mode = unix.S_IFREG // treat as file, not directory
			return nil
		}
		openSyscall = func(path string, flags int, mode uint32) (int, error) {
			return 10, nil
		}
		closeSyscall = func(fd int) error {
			return nil
		}

		dev := specs.LinuxDevice{Path: "/dev/kvm"}
		err := setupDev("/tmp/mon", dev)
		assert.NoError(t, err)
		assert.Len(t, mounts, 1)
		assert.Equal(t, "/dev/kvm", mounts[0].source)
		assert.Equal(t, "/tmp/mon/dev/kvm", mounts[0].target)
	})

	t.Run("regular mknod path", func(t *testing.T) {
		runningInUserNSHook = func() bool { return false }
		mkdirAllCalled := false
		mkdirAllHook = func(path string, perm os.FileMode) error {
			mkdirAllCalled = true
			return nil
		}
		mknodCalled := false
		mknodSyscall = func(path string, mode uint32, dev int) error {
			mknodCalled = true
			return nil
		}
		chmodCalled := false
		chmodSyscall = func(path string, mode uint32) error {
			chmodCalled = true
			return nil
		}
		chownCalled := false
		chownSyscall = func(path string, uid, gid int) error {
			chownCalled = true
			return nil
		}

		mode := os.FileMode(0600)
		dev := specs.LinuxDevice{
			Path:     "/dev/subfolder/kvm",
			Type:     "c",
			FileMode: &mode,
			UID:      new(uint32),
			GID:      new(uint32),
		}

		err := setupDev("/tmp/mon", dev)
		assert.NoError(t, err)
		assert.True(t, mkdirAllCalled)
		assert.True(t, mknodCalled)
		assert.True(t, chmodCalled)
		assert.True(t, chownCalled)
	})

	t.Run("invalid device type", func(t *testing.T) {
		dev := specs.LinuxDevice{
			Path: "/dev/invalid",
			Type: "invalid",
		}
		err := setupDev("/tmp/mon", dev)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "is not a device node")
	})
}

func TestFileFromHost(t *testing.T) {
	origStat := statSyscall
	origSecureJoin := secureJoinHook
	origCopyFile := copyFileHook
	origChmod := chmodSyscall
	origChown := chownSyscall
	origMount := mountSyscall
	t.Cleanup(func() {
		statSyscall = origStat
		secureJoinHook = origSecureJoin
		copyFileHook = origCopyFile
		chmodSyscall = origChmod
		chownSyscall = origChown
		mountSyscall = origMount
	})

	t.Run("error statting host path", func(t *testing.T) {
		statSyscall = func(path string, stat *unix.Stat_t) error {
			return errors.New("stat error")
		}
		err := fileFromHost("/tmp/mon", "/nonexistent", "")
		assert.Error(t, err)
	})

	t.Run("cannot copy directory", func(t *testing.T) {
		statSyscall = func(path string, stat *unix.Stat_t) error {
			stat.Mode = unix.S_IFDIR
			return nil
		}
		err := fileFromHost("/tmp/mon", "/tmp/dir", "")
		assert.ErrorIs(t, err, ErrCopyDir)
	})

	t.Run("happy path file copy", func(t *testing.T) {
		statSyscall = func(path string, stat *unix.Stat_t) error {
			stat.Mode = unix.S_IFREG | 0644
			return nil
		}
		secureJoinHook = func(root, path string) (string, error) {
			return filepath.Join(root, path), nil
		}
		copyCalled := false
		copyFileHook = func(source, target string) error {
			copyCalled = true
			return nil
		}
		chmodCalled := false
		chmodSyscall = func(path string, mode uint32) error {
			chmodCalled = true
			return nil
		}
		chownCalled := false
		chownSyscall = func(path string, uid, gid int) error {
			chownCalled = true
			return nil
		}

		err := fileFromHost("/tmp/mon", "/etc/passwd", "etc/passwd")
		assert.NoError(t, err)
		assert.True(t, copyCalled)
		assert.True(t, chmodCalled)
		assert.True(t, chownCalled)
	})
}

func TestGetUnprivilegedMountFlags(t *testing.T) {
	origStatfs := statfsSyscall
	t.Cleanup(func() { statfsSyscall = origStatfs })

	t.Run("statfs fails", func(t *testing.T) {
		statfsSyscall = func(path string, stat *unix.Statfs_t) error {
			return errors.New("statfs error")
		}
		_, err := getUnprivilegedMountFlags("/tmp/path")
		assert.Error(t, err)
	})

	t.Run("extract flags correctly", func(t *testing.T) {
		statfsSyscall = func(path string, stat *unix.Statfs_t) error {
			stat.Flags = int64(unix.ST_RDONLY | unix.ST_NOSUID | unix.ST_RELATIME)
			return nil
		}
		flags, err := getUnprivilegedMountFlags("/tmp/path")
		assert.NoError(t, err)
		assert.Equal(t, uintptr(unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_RELATIME), flags)
	})

	t.Run("fallback to strictatime", func(t *testing.T) {
		statfsSyscall = func(path string, stat *unix.Statfs_t) error {
			stat.Flags = 0 // no noatime, no relatime
			return nil
		}
		flags, err := getUnprivilegedMountFlags("/tmp/path")
		assert.NoError(t, err)
		assert.Equal(t, uintptr(unix.MS_STRICTATIME), flags)
	})
}

func TestRootfsParentMountPrivate(t *testing.T) {
	origMount := mountSyscall
	t.Cleanup(func() { mountSyscall = origMount })

	t.Run("already private", func(t *testing.T) {
		mountCalls := 0
		mountSyscall = func(source, target, fstype string, flags uintptr, data string) error {
			mountCalls++
			return nil
		}
		err := rootfsParentMountPrivate("/tmp/mon/rootfs")
		assert.NoError(t, err)
		assert.Equal(t, 1, mountCalls)
	})

	t.Run("traverse up to find mount point", func(t *testing.T) {
		mountCalls := 0
		mountSyscall = func(source, target, fstype string, flags uintptr, data string) error {
			mountCalls++
			if target == "/tmp/mon/rootfs" || target == "/tmp/mon" {
				return unix.EINVAL // not a mount point
			}
			return nil
		}
		err := rootfsParentMountPrivate("/tmp/mon/rootfs")
		assert.NoError(t, err)
		// Should try /tmp/mon/rootfs, then /tmp/mon, then /tmp, and succeed
		assert.Equal(t, 3, mountCalls)
	})
}

func TestPrepareRoot(t *testing.T) {
	origMount := mountSyscall
	t.Cleanup(func() { mountSyscall = origMount })

	t.Run("success", func(t *testing.T) {
		var calls []mountCall
		mountSyscall = func(source, target, fstype string, flags uintptr, data string) error {
			calls = append(calls, mountCall{source, target, fstype, flags, data})
			return nil
		}

		err := prepareRoot("/tmp/mon/rootfs", "shared")
		assert.NoError(t, err)
		assert.Len(t, calls, 3)
		assert.Equal(t, "/", calls[0].target)
		assert.Equal(t, uintptr(unix.MS_SHARED), calls[0].flags)
		assert.Equal(t, "/tmp/mon/rootfs", calls[2].source)
		assert.Equal(t, "/tmp/mon/rootfs", calls[2].target)
		assert.Equal(t, "bind", calls[2].fstype)
	})
}

func TestApplyMounts(t *testing.T) {
	origSecureJoin := secureJoinHook
	origMkdirAll := mkdirAllHook
	origContainerdMount := containerdMountHook
	t.Cleanup(func() {
		secureJoinHook = origSecureJoin
		mkdirAllHook = origMkdirAll
		containerdMountHook = origContainerdMount
	})

	t.Run("empty mounts returns nil", func(t *testing.T) {
		err := applyMounts("/tmp/mon", nil)
		assert.NoError(t, err)
	})

	t.Run("fail applying mount", func(t *testing.T) {
		secureJoinHook = func(root, path string) (string, error) {
			return "", errors.New("join error")
		}
		mounts := []specs.Mount{{Source: "/src", Destination: "/dst"}}
		err := applyMounts("/tmp/mon", mounts)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to apply mount")
	})
}

func TestApplyMount(t *testing.T) {
	origSecureJoin := secureJoinHook
	origMkdirAll := mkdirAllHook
	origContainerdMount := containerdMountHook
	origRunningInUserNS := runningInUserNSHook
	origStatfs := statfsSyscall
	origMount := mountSyscall
	origChmod := osChmodHook
	origStat := statSyscall
	t.Cleanup(func() {
		secureJoinHook = origSecureJoin
		mkdirAllHook = origMkdirAll
		containerdMountHook = origContainerdMount
		runningInUserNSHook = origRunningInUserNS
		statfsSyscall = origStatfs
		mountSyscall = origMount
		osChmodHook = origChmod
		statSyscall = origStat
	})

	statSyscall = func(path string, stat *unix.Stat_t) error {
		stat.Mode = unix.S_IFDIR | 0755 // default to directory
		return nil
	}

	t.Run("basic mount success", func(t *testing.T) {
		secureJoinHook = func(root, path string) (string, error) {
			return filepath.Join(root, path), nil
		}
		mkdirAllHook = func(path string, perm os.FileMode) error { return nil }
		mountCalled := false
		containerdMountHook = func(m *mount.Mount, target string) error {
			mountCalled = true
			assert.Equal(t, "tmpfs", m.Type)
			assert.Equal(t, "/tmp/mon/tmp", target)
			return nil
		}

		m := specs.Mount{Type: "tmpfs", Destination: "/tmp"}
		err := applyMount("/tmp/mon", m)
		assert.NoError(t, err)
		assert.True(t, mountCalled)
	})

	t.Run("bind mount remount flags", func(t *testing.T) {
		secureJoinHook = func(root, path string) (string, error) {
			return filepath.Join(root, path), nil
		}
		mkdirAllHook = func(path string, perm os.FileMode) error { return nil }
		containerdMountHook = func(m *mount.Mount, target string) error { return nil }
		runningInUserNSHook = func() bool { return false }
		var calls []mountCall
		mountSyscall = func(source, target, fstype string, flags uintptr, data string) error {
			calls = append(calls, mountCall{source, target, fstype, flags, data})
			return nil
		}

		m := specs.Mount{
			Type:        "bind",
			Source:      "/src",
			Destination: "/dst",
			Options:     []string{"ro", "nodev"},
		}
		err := applyMount("/tmp/mon", m)
		assert.NoError(t, err)
		assert.Len(t, calls, 1)
		assert.Equal(t, "/tmp/mon/dst", calls[0].target)
		assert.Equal(t, uintptr(unix.MS_RDONLY|unix.MS_NODEV|unix.MS_BIND|unix.MS_REMOUNT), calls[0].flags)
	})

	t.Run("user ns unprivileged flag merging", func(t *testing.T) {
		secureJoinHook = func(root, path string) (string, error) {
			return filepath.Join(root, path), nil
		}
		mkdirAllHook = func(path string, perm os.FileMode) error { return nil }
		containerdMountHook = func(m *mount.Mount, target string) error { return nil }
		runningInUserNSHook = func() bool { return true }
		statfsSyscall = func(path string, stat *unix.Statfs_t) error {
			stat.Flags = int64(unix.ST_NOSUID)
			return nil
		}
		var calls []mountCall
		mountSyscall = func(source, target, fstype string, flags uintptr, data string) error {
			calls = append(calls, mountCall{source, target, fstype, flags, data})
			return nil
		}

		m := specs.Mount{
			Type:        "bind",
			Source:      "/src",
			Destination: "/dst",
			Options:     []string{"ro"},
		}
		err := applyMount("/tmp/mon", m)
		assert.NoError(t, err)
		assert.Len(t, calls, 1)
		// Should have MS_RDONLY (from spec options) and MS_NOSUID (from Statfs)
		assert.Equal(t, uintptr(unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_BIND|unix.MS_REMOUNT|unix.MS_STRICTATIME), calls[0].flags)
	})

	t.Run("tmpfs sticky bit chmod", func(t *testing.T) {
		secureJoinHook = func(root, path string) (string, error) {
			return filepath.Join(root, path), nil
		}
		mkdirAllHook = func(path string, perm os.FileMode) error { return nil }
		containerdMountHook = func(m *mount.Mount, target string) error { return nil }
		chmodCalled := false
		osChmodHook = func(name string, mode os.FileMode) error {
			chmodCalled = true
			assert.Equal(t, os.FileMode(0777)|os.ModeSticky, mode)
			return nil
		}

		m := specs.Mount{
			Type:        "tmpfs",
			Destination: "/tmp",
			Options:     []string{"mode=1777"},
		}
		err := applyMount("/tmp/mon", m)
		assert.NoError(t, err)
		assert.True(t, chmodCalled)
	})
}

func TestCreateMountPoint(t *testing.T) {
	origStat := statSyscall
	origMkdirAll := mkdirAllHook
	origOpen := openSyscall
	origClose := closeSyscall
	t.Cleanup(func() {
		statSyscall = origStat
		mkdirAllHook = origMkdirAll
		openSyscall = origOpen
		closeSyscall = origClose
	})

	t.Run("bind mount directory", func(t *testing.T) {
		statSyscall = func(path string, stat *unix.Stat_t) error {
			stat.Mode = unix.S_IFDIR
			return nil
		}
		mkdirAllCalled := false
		mkdirAllHook = func(path string, perm os.FileMode) error {
			mkdirAllCalled = true
			assert.Equal(t, "/tmp/mon/dst", path)
			return nil
		}

		m := specs.Mount{Type: "bind", Source: "/src"}
		err := createMountPoint("/tmp/mon/dst", m)
		assert.NoError(t, err)
		assert.True(t, mkdirAllCalled)
	})

	t.Run("bind mount file", func(t *testing.T) {
		statSyscall = func(path string, stat *unix.Stat_t) error {
			stat.Mode = unix.S_IFREG
			return nil
		}
		mkdirAllHook = func(path string, perm os.FileMode) error { return nil }
		openCalled := false
		openSyscall = func(path string, flags int, mode uint32) (int, error) {
			openCalled = true
			assert.Equal(t, "/tmp/mon/dst", path)
			return 10, nil
		}
		closeCalled := false
		closeSyscall = func(fd int) error {
			closeCalled = true
			assert.Equal(t, 10, fd)
			return nil
		}

		m := specs.Mount{Type: "bind", Source: "/src"}
		err := createMountPoint("/tmp/mon/dst", m)
		assert.NoError(t, err)
		assert.True(t, openCalled)
		assert.True(t, closeCalled)
	})

	t.Run("non-bind mount mkdir target", func(t *testing.T) {
		mkdirAllCalled := false
		mkdirAllHook = func(path string, perm os.FileMode) error {
			mkdirAllCalled = true
			assert.Equal(t, "/tmp/mon/dst", path)
			return nil
		}

		m := specs.Mount{Type: "tmpfs"}
		err := createMountPoint("/tmp/mon/dst", m)
		assert.NoError(t, err)
		assert.True(t, mkdirAllCalled)
	})
}

func TestSplitMountOptions(t *testing.T) {
	t.Run("separate options correctly", func(t *testing.T) {
		opts, prop, vfs := splitMountOptions([]string{"ro", "rshared", "nodev", "sync"}, true)
		assert.Empty(t, opts) // for bind mounts, VFS options "ro", "nodev", "sync" are withheld
		assert.Equal(t, []int{unix.MS_SHARED | unix.MS_REC}, prop)
		assert.Equal(t, uintptr(unix.MS_RDONLY|unix.MS_NODEV|unix.MS_SYNCHRONOUS), vfs)
	})

	t.Run("non-bind keeps VFS flags", func(t *testing.T) {
		opts, prop, vfs := splitMountOptions([]string{"ro", "rshared", "nodev", "sync"}, false)
		assert.Equal(t, []string{"ro", "nodev", "sync"}, opts)
		assert.Equal(t, []int{unix.MS_SHARED | unix.MS_REC}, prop)
		assert.Equal(t, uintptr(unix.MS_RDONLY|unix.MS_NODEV|unix.MS_SYNCHRONOUS), vfs)
	})
}
