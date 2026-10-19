//go:build linux

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
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/moby/sys/mountinfo"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

func TestParseOverlayOptions(t *testing.T) {
	t.Parallel()

	l, err := parseOverlayOptions("rw,lowerdir=/s/snapshots/3/fs:/s/snapshots/2/fs:/s/snapshots/1/fs," +
		"upperdir=/s/snapshots/4/fs,workdir=/s/snapshots/4/work,uuid=on,userxattr")
	require.NoError(t, err)
	assert.Equal(t, []string{"/s/snapshots/3/fs", "/s/snapshots/2/fs", "/s/snapshots/1/fs"}, l.lowers)
	assert.Equal(t, "/s/snapshots/4/fs", l.upper)
	assert.True(t, l.userxattr)

	l, err = parseOverlayOptions("rw,lowerdir+=/s/snapshots/2/fs,lowerdir+=/s/snapshots/1/fs,upperdir=/s/snapshots/4/fs")
	require.NoError(t, err)
	assert.Equal(t, []string{"/s/snapshots/2/fs", "/s/snapshots/1/fs"}, l.lowers)
	assert.False(t, l.userxattr)

	for _, opts := range []string{
		"rw,upperdir=/s/snapshots/4/fs",
		"rw,lowerdir=/s/snapshots/1/fs",
		// containerd's relative layers for a long list
		"rw,lowerdir=3/fs:2/fs,upperdir=/s/snapshots/4/fs",
		// an escaped colon
		`rw,lowerdir=/s/a\:b,upperdir=/s/snapshots/4/fs`,
	} {
		_, err = parseOverlayOptions(opts)
		assert.Error(t, err, opts)
	}
}

func TestFindOverlayLayout(t *testing.T) {
	t.Parallel()

	all := []*mountinfo.Info{
		{Mountpoint: "/run/c/rootfs", FSType: "overlay", VFSOptions: "rw,lowerdir=/s/snapshots/1/fs,upperdir=/s/snapshots/2/fs"},
		{Mountpoint: "/run/d/rootfs", FSType: "ext4", VFSOptions: "rw"},
	}
	l, err := findOverlayLayout(all, "/run/c/rootfs")
	require.NoError(t, err)
	assert.Equal(t, "/s/snapshots/2/fs", l.upper)

	_, err = findOverlayLayout(all, "/run/d/rootfs")
	assert.ErrorContains(t, err, "overlayfs snapshotter")
	_, err = findOverlayLayout(all, "/run/e/rootfs")
	assert.ErrorContains(t, err, "not a mount point")
}

// The upper disk goes next to the snapshot only in the overlayfs
// snapshotter's own layout. Anything else is refused, not written into.
func TestOverlayBlockDirs(t *testing.T) {
	t.Parallel()

	snap, cache, err := overlayBlockDirs("/var/lib/c/snapshotter/snapshots/42/fs", "")
	require.NoError(t, err)
	assert.Equal(t, "/var/lib/c/snapshotter/snapshots/42", snap)
	assert.Equal(t, "/var/lib/c/snapshotter/urunc-rootfs-cache", cache)

	_, cache, err = overlayBlockDirs("/var/lib/c/snapshotter/snapshots/42/fs", "/srv/cache")
	require.NoError(t, err)
	assert.Equal(t, "/srv/cache", cache)

	for _, upper := range []string{"/var/lib/c/snapshotter/snapshots/42/upper", "/home/u/upper/fs", "/fs"} {
		_, _, err = overlayBlockDirs(upper, "")
		assert.Error(t, err, upper)
	}
}

func TestOverlayBlockVolumes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	data := filepath.Join(dir, "data")
	require.NoError(t, os.Mkdir(proj, 0o755))
	require.NoError(t, os.Mkdir(data, 0o755))
	resolv := filepath.Join(dir, "resolv.conf")
	hosts := filepath.Join(dir, "hosts")
	other := filepath.Join(dir, "other")
	require.NoError(t, os.WriteFile(resolv, []byte("nameserver 10.4.0.1\n"), 0o644))
	require.NoError(t, os.WriteFile(hosts, []byte("127.0.0.1 localhost\n"), 0o644))
	require.NoError(t, os.WriteFile(other, []byte("x"), 0o644))

	exports, files, err := overlayBlockVolumes([]specs.Mount{
		{Type: "proc", Source: "proc", Destination: "/proc"},
		{Type: "bind", Source: proj, Destination: "/work/proj", Options: []string{"rbind", "rw"}},
		{Type: "bind", Source: data, Destination: "/data/", Options: []string{"rbind", "ro"}},
		{Type: "bind", Source: resolv, Destination: "/etc/resolv.conf", Options: []string{"rbind"}},
		{Type: "bind", Source: hosts, Destination: "/etc/hosts", Options: []string{"rbind"}},
		{Type: "bind", Source: other, Destination: "/etc/other", Options: []string{"rbind"}},
	})
	require.NoError(t, err)
	require.Len(t, exports, 2)
	assert.Equal(t, "share0", exports[0].tag)
	assert.Equal(t, "/urunc-shares/0", exports[0].share)
	assert.Equal(t, "/work/proj", exports[0].dest)
	assert.False(t, exports[0].ro)
	assert.Equal(t, "share1", exports[1].tag)
	assert.Equal(t, "/data", exports[1].dest)
	assert.True(t, exports[1].ro)
	assert.Equal(t, []types.InitrdFile{
		{Name: "/urunc-resolv.conf", Data: "nameserver 10.4.0.1\n"},
		{Name: "/urunc-hosts", Data: "127.0.0.1 localhost\n"},
	}, files)

	for _, dest := range []string{"/", "relative", "/a\tb", "/a\nb"} {
		_, _, err = overlayBlockVolumes([]specs.Mount{{Type: "bind", Source: proj, Destination: dest, Options: []string{"rbind"}}})
		assert.Error(t, err, "%q", dest)
	}
	_, _, err = overlayBlockVolumes([]specs.Mount{{Type: "bind", Source: filepath.Join(dir, "missing"), Destination: "/m", Options: []string{"rbind"}}})
	assert.Error(t, err)
}

func TestOverlayBlockGuestFiles(t *testing.T) {
	t.Parallel()

	o := &overlayBlockRootfs{
		vfsdConfig:   types.ExtraBinConfig{Path: "/usr/libexec/virtiofsd", Options: "--cache always --sandbox none"},
		containerUID: 1000,
		containerGID: 1000,
		exports: []overlayBlockExport{
			{tag: "share0", share: "/urunc-shares/0", dest: "/work/proj", owner: bindMountOwner{uid: 1001, gid: 1000}},
			{tag: "share1", share: "/urunc-shares/1", dest: "/data", ro: true, owner: bindMountOwner{uid: 1000, gid: 1000}},
		},
		files: []types.InitrdFile{{Name: "/urunc-hosts", Data: "127.0.0.1 localhost\n"}},
	}
	exports, cmds := o.virtiofsExports()
	assert.Equal(t, []types.VirtiofsExport{
		{Tag: "share0", Socket: "/tmp/vfs-share0.sock"},
		{Tag: "share1", Socket: "/tmp/vfs-share1.sock"},
	}, exports)
	assert.Equal(t, [][]string{
		{"/usr/libexec/virtiofsd", "--socket-path=/tmp/vfs-share0.sock", "--shared-dir", "/urunc-shares/0",
			"--cache", "always", "--sandbox", "none", "--translate-uid", "map:1000:1001:1"},
		{"/usr/libexec/virtiofsd", "--socket-path=/tmp/vfs-share1.sock", "--shared-dir", "/urunc-shares/1",
			"--cache", "always", "--sandbox", "none"},
	}, cmds)
	// Exec waits for each daemon's socket before the monitor starts.
	for i, c := range cmds {
		assert.Equal(t, exports[i].Socket, vhostSocketPath(c))
	}

	assert.Equal(t, []types.InitrdFile{
		{Name: "/urunc-rootfs-lower", Data: "block:disk0:ext4\n"},
		{Name: "/urunc-rootfs-upper", Data: "block:disk1\n"},
		{Name: "/urunc-mounts", Data: "share0\t/work/proj\trw\nshare1\t/data\tro\n"},
		{Name: "/urunc-hosts", Data: "127.0.0.1 localhost\n"},
	}, o.initrdFiles())

	blocks, err := o.getBlockDevs()
	require.NoError(t, err)
	assert.Equal(t, []types.BlockDevParams{
		{ID: "disk0", Source: overlayBlockLowerPath, ReadOnly: true},
		{ID: "disk1", Source: overlayBlockUpperPath},
	}, blocks)

	// A read-only volume is a read-only bind in the monitor rootfs, so
	// virtiofsd cannot write it whatever the guest asks.
	o.lower, o.upper = "/cache/k.ext4", "/snap/42/urunc-rootfs-upper.ext4"
	o.bootKernelHost, o.bootInitrdHost = "/boot/bzImage", "/boot/initrd"
	o.exports[0].source, o.exports[1].source = "/home/u/proj", "/srv/data"
	mounts, err := o.getMounts()
	require.NoError(t, err)
	byDest := map[string]specs.Mount{}
	for _, m := range mounts {
		byDest[m.Destination] = m
	}
	assert.Contains(t, byDest[overlayBlockLowerPath].Options, "ro")
	assert.NotContains(t, byDest[overlayBlockUpperPath].Options, "ro")
	assert.Contains(t, byDest["/urunc-shares/1"].Options, "ro")
	assert.NotContains(t, byDest["/urunc-shares/0"].Options, "ro")
	assert.Equal(t, "/srv/data", byDest["/urunc-shares/1"].Source)
	assert.Contains(t, byDest, "/usr/libexec/virtiofsd")
}

func requireMke2fs(t *testing.T) string {
	t.Helper()
	for _, p := range []string{"/usr/sbin/mke2fs", "/sbin/mke2fs"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("mke2fs is not installed")
	return ""
}

// The lower is built once per list of layers, and concurrent containers of
// one image build it once.
func TestEnsureLowerImage(t *testing.T) {
	t.Parallel()
	mke2fs := requireMke2fs(t)

	layer := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(layer, "etc"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(layer, "etc", "os-release"), []byte("ID=test\n"), 0o644))
	cache := filepath.Join(t.TempDir(), "cache")
	l := overlayLayout{lowers: []string{layer}, upper: "/unused"}

	var wg sync.WaitGroup
	disks := make([]string, 4)
	errs := make([]error, 4)
	for i := range disks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			disks[i], errs[i] = ensureLowerImage(mke2fs, cache, l)
		}(i)
	}
	wg.Wait()
	for i := range disks {
		require.NoError(t, errs[i])
		assert.Equal(t, disks[0], disks[i])
	}
	info, err := os.Stat(disks[0])
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), info.Mode().Perm())
	entries, err := os.ReadDir(cache)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "staging file left behind")
	}

	if out, err := exec.Command("debugfs", "-R", "cat /etc/os-release", disks[0]).CombinedOutput(); err == nil {
		assert.Contains(t, string(out), "ID=test")
	}
}

func TestEnsureUpperImage(t *testing.T) {
	t.Parallel()
	mke2fs := requireMke2fs(t)

	snap := t.TempDir()
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	disk, err := ensureUpperImage(mke2fs, snap, uid, gid)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(snap, overlayBlockUpperName), disk)
	info, err := os.Stat(disk)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Equal(t, int64(16)<<30, info.Size(), "16 GiB apparent")

	// A second boot of the container reuses it, with what the guest wrote.
	require.NoError(t, os.WriteFile(disk, []byte("kept"), 0o600))
	again, err := ensureUpperImage(mke2fs, snap, uid, gid)
	require.NoError(t, err)
	got, err := os.ReadFile(again)
	require.NoError(t, err)
	assert.Equal(t, "kept", string(got))
}

func TestOverlayBlockSelection(t *testing.T) {
	t.Parallel()

	boot := map[string]string{
		annotBootKernel:  "/opt/urunc/boot/bzImage",
		annotBootInitrd:  "/opt/urunc/boot/container-initrd",
		annotMountRootfs: "true",
		annotRootfsMode:  RootfsModeOverlayBlock,
	}
	linux, err := unikernels.New(unikernels.LinuxUnikernel)
	require.NoError(t, err)
	rs := &rootfsSelector{annot: boot, cntrRootfs: "/run/c/rootfs", unikernel: linux}
	res, err := rs.overlayBlock()
	require.NoError(t, err)
	assert.Equal(t, rootfsOverlayBlock, res.Type)
	assert.Equal(t, "/run/c/rootfs", res.MountedPath)
	assert.Equal(t, "ext4", res.FsType)

	noBoot := map[string]string{annotMountRootfs: "true", annotRootfsMode: RootfsModeOverlayBlock}
	_, err = (&rootfsSelector{annot: noBoot, unikernel: linux}).overlayBlock()
	assert.Error(t, err)
}

func TestValidateRootfsMode(t *testing.T) {
	t.Parallel()

	for _, mon := range []hypervisors.VmmType{hypervisors.HviVmm, hypervisors.CloudHypervisorVmm, hypervisors.QemuVmm} {
		config := getConfigFromSpec(containerBootSpec(map[string]string{
			annotHypervisor: string(mon),
			annotRootfsMode: RootfsModeOverlayBlock,
		}))
		require.NoError(t, config.validateValues(), mon)
		assert.Equal(t, RootfsModeOverlayBlock, config.Map()[annotRootfsMode], "the mode must reach the state")
	}

	config := getConfigFromSpec(containerBootSpec(map[string]string{
		annotHypervisor: string(hypervisors.FirecrackerVmm),
		annotRootfsMode: RootfsModeOverlayBlock,
	}))
	assert.ErrorContains(t, config.validateValues(), "rootfsMode")

	config = getConfigFromSpec(containerBootSpec(map[string]string{annotRootfsMode: "overlay"}))
	assert.ErrorContains(t, config.validateValues(), "rootfsMode")

	config = getConfigFromSpec(&specs.Spec{Annotations: map[string]string{
		annotType:       unikernels.LinuxUnikernel,
		annotHypervisor: string(hypervisors.QemuVmm),
		annotBinary:     "/kernel",
		annotRootfsMode: RootfsModeOverlayBlock,
	}})
	assert.ErrorContains(t, config.validateValues(), "generic container boot")
}
