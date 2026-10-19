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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/moby/sys/mountinfo"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urunc-dev/urunc/internal/constants"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	// The disks and the exports of an overlay-block root inside the
	// monitor rootfs.
	overlayBlockLowerPath = "/urunc-disks/rootfs-lower.ext4"
	overlayBlockUpperPath = "/urunc-disks/rootfs-upper.ext4"
	overlayBlockShareDir  = "/urunc-shares"
	// overlayBlockUpperName is the upper disk, kept in the container's
	// snapshot directory so that it lives as long as the container: it
	// survives a stop and goes when containerd removes the snapshot.
	overlayBlockUpperName = "urunc-rootfs-upper.ext4"
	// overlayBlockUpperSize is the apparent size of the upper disk. The file
	// is sparse, so it costs only what the guest writes, and it caps what
	// one container can add on top of its image.
	overlayBlockUpperSize = "16G"
	// overlayBlockCacheName is the default directory of the lower images,
	// next to the overlayfs snapshotter's snapshots.
	overlayBlockCacheName = "urunc-rootfs-cache"
	// The guest finds the disks by these serials. The lower is the first
	// disk and the upper the second, on every monitor.
	overlayBlockLowerSerial = "disk0"
	overlayBlockUpperSerial = "disk1"
	ext4BlockSize           = 4096
)

// overlayBlockRootfs boots a generic container from two disks: a read-only
// ext4 image of the container image, built once per image and cached, and a
// writable ext4 upper per container. The guest's /init mounts them as an
// overlay, as the /urunc-rootfs-lower and /urunc-rootfs-upper files of its
// initrd say. The directory volumes go to the guest as virtio-fs exports,
// one virtiofsd each, and /etc/resolv.conf, /etc/hosts and /etc/hostname
// as files of the initrd.
type overlayBlockRootfs struct {
	mounts []specs.Mount
	// mountedPath is the container rootfs, an overlayfs snapshot. Its
	// lowerdirs are the image layers the lower is built from.
	mountedPath string
	cacheDir    string
	mke2fs      string
	vfsdConfig  types.ExtraBinConfig
	memory      uint64
	// bootKernelHost and bootInitrdHost are the host kernel and boot initrd
	// of the generic container boot.
	bootKernelHost string
	bootInitrdHost string
	containerUID   uint32
	containerGID   uint32

	// Set by preSetup.
	lower   string
	upper   string
	exports []overlayBlockExport
	files   []types.InitrdFile
}

// overlayBlockExport is one directory volume, served by its own virtiofsd.
type overlayBlockExport struct {
	tag    string
	source string // the host directory
	share  string // where it is bind-mounted in the monitor rootfs
	dest   string // the mount point in the guest
	ro     bool
	owner  bindMountOwner
}

// overlayLayout is what the container rootfs mount says about the snapshot.
type overlayLayout struct {
	lowers    []string // top layer first
	upper     string
	userxattr bool
}

func (o *overlayBlockRootfs) preSetup() error {
	for key, path := range map[string]string{annotBootKernel: o.bootKernelHost, annotBootInitrd: o.bootInitrdHost} {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular file, got %s", key, path)
		}
	}

	all, err := mountinfo.GetMounts(nil)
	if err != nil {
		return fmt.Errorf("failed to read mountinfo: %w", err)
	}
	layout, err := findOverlayLayout(all, o.mountedPath)
	if err != nil {
		return err
	}
	snapshotDir, cacheDir, err := overlayBlockDirs(layout.upper, o.cacheDir)
	if err != nil {
		return err
	}

	o.lower, err = ensureLowerImage(o.mke2fs, cacheDir, layout)
	if err != nil {
		return err
	}
	o.upper, err = ensureUpperImage(o.mke2fs, snapshotDir, o.containerUID, o.containerGID)
	if err != nil {
		return err
	}

	o.exports, o.files, err = overlayBlockVolumes(o.mounts)
	if err != nil {
		return err
	}
	if len(o.exports) > 0 && !fileExists(o.vfsdConfig.Path) {
		return fmt.Errorf("%s=%s needs virtiofsd for its directory volumes, and %q is missing",
			annotRootfsMode, RootfsModeOverlayBlock, o.vfsdConfig.Path)
	}
	return nil
}

func (o *overlayBlockRootfs) postSetup() error {
	return nil
}

func (o *overlayBlockRootfs) getMounts() ([]specs.Mount, error) {
	mounts := []specs.Mount{
		// The vhost-user sockets go in /tmp, and qemu keeps the guest memory
		// there for virtiofsd to map.
		tmpfsMount("/tmp", chooseTmpfsSize("virtiofs", o.memory)),
		bindMount(o.bootKernelHost, constants.ContainerBootKernelPath, true, true, "nodev", "nosuid", "noexec"),
		bindMount(o.bootInitrdHost, constants.ContainerBootInitrdPath, true, true, "nodev", "nosuid", "noexec"),
		bindMount(o.lower, overlayBlockLowerPath, true, true, "nodev", "nosuid", "noexec"),
		bindMount(o.upper, overlayBlockUpperPath, true, false, "nodev", "nosuid", "noexec"),
	}
	if len(o.exports) > 0 {
		mounts = append(mounts, bindMount(o.vfsdConfig.Path, o.vfsdConfig.Path, true, true))
	}
	// A read-only volume is read-only in the monitor rootfs, so virtiofsd
	// cannot write it whatever the guest asks.
	for _, e := range o.exports {
		mounts = append(mounts, bindMount(e.source, e.share, true, e.ro))
	}
	return mounts, nil
}

func (o *overlayBlockRootfs) getBlockDevs() ([]types.BlockDevParams, error) {
	return []types.BlockDevParams{
		{ID: overlayBlockLowerSerial, Source: overlayBlockLowerPath, ReadOnly: true},
		{ID: overlayBlockUpperSerial, Source: overlayBlockUpperPath},
	}, nil
}

func (o *overlayBlockRootfs) getSharedDirs() (types.SharedfsParams, error) {
	return types.SharedfsParams{}, nil
}

func (o *overlayBlockRootfs) preStartCmd() []string {
	return nil
}

// virtiofsExports returns the exports of the directory volumes and the
// virtiofsd command line that serves each one.
func (o *overlayBlockRootfs) virtiofsExports() ([]types.VirtiofsExport, [][]string) {
	var exports []types.VirtiofsExport
	var cmds [][]string
	for _, e := range o.exports {
		sock := "/tmp/vfs-" + e.tag + ".sock"
		exports = append(exports, types.VirtiofsExport{Tag: e.tag, Socket: sock})
		argv := []string{o.vfsdConfig.Path, "--socket-path=" + sock, "--shared-dir", e.share}
		if o.vfsdConfig.Options != "" {
			argv = append(argv, strings.Fields(o.vfsdConfig.Options)...)
		}
		argv = append(argv, virtiofsIDTranslations([]bindMountOwner{e.owner}, o.containerUID, o.containerGID)...)
		cmds = append(cmds, argv)
	}
	return exports, cmds
}

// initrdFiles returns the files the guest's initrd gets: the two layers, the
// volume mounts and the network files.
func (o *overlayBlockRootfs) initrdFiles() []types.InitrdFile {
	files := []types.InitrdFile{
		{Name: "/urunc-rootfs-lower", Data: "block:" + overlayBlockLowerSerial + ":ext4\n"},
		{Name: "/urunc-rootfs-upper", Data: "block:" + overlayBlockUpperSerial + "\n"},
	}
	if len(o.exports) > 0 {
		var b strings.Builder
		for _, e := range o.exports {
			mode := "rw"
			if e.ro {
				mode = "ro"
			}
			b.WriteString(e.tag + "\t" + e.dest + "\t" + mode + "\n")
		}
		files = append(files, types.InitrdFile{Name: "/urunc-mounts", Data: b.String()})
	}
	return append(files, o.files...)
}

// findOverlayLayout returns the layers of the overlayfs mounted at path.
func findOverlayLayout(all []*mountinfo.Info, path string) (overlayLayout, error) {
	var m *mountinfo.Info
	for _, i := range all {
		if i.Mountpoint == path {
			m = i
		}
	}
	if m == nil {
		return overlayLayout{}, fmt.Errorf("%s=%s: the container rootfs %s is not a mount point",
			annotRootfsMode, RootfsModeOverlayBlock, path)
	}
	if m.FSType != "overlay" {
		return overlayLayout{}, fmt.Errorf("%s=%s needs the overlayfs snapshotter, but the container rootfs is %s",
			annotRootfsMode, RootfsModeOverlayBlock, m.FSType)
	}
	return parseOverlayOptions(m.VFSOptions)
}

// parseOverlayOptions reads the layers out of overlayfs super options, in
// either the lowerdir=a:b form or the lowerdir+= form of the new mount API.
func parseOverlayOptions(opts string) (overlayLayout, error) {
	var l overlayLayout
	for _, o := range strings.Split(opts, ",") {
		key, val, _ := strings.Cut(o, "=")
		switch key {
		case "lowerdir":
			l.lowers = append(l.lowers, strings.Split(val, ":")...)
		case "lowerdir+":
			l.lowers = append(l.lowers, val)
		case "upperdir":
			l.upper = val
		case "userxattr":
			l.userxattr = true
		}
	}
	if len(l.lowers) == 0 || l.upper == "" {
		return overlayLayout{}, fmt.Errorf("%s=%s: the container rootfs has no lowerdir or upperdir (%q)",
			annotRootfsMode, RootfsModeOverlayBlock, opts)
	}
	// containerd passes relative layers when the list would not fit in a
	// page, and overlayfs escapes a comma or colon with a backslash. Neither
	// can be mounted again from here.
	for _, p := range append([]string{l.upper}, l.lowers...) {
		if !filepath.IsAbs(p) || strings.ContainsRune(p, '\\') {
			return overlayLayout{}, fmt.Errorf("%s=%s: cannot use the snapshot layer %q",
				annotRootfsMode, RootfsModeOverlayBlock, p)
		}
	}
	return l, nil
}

// overlayBlockDirs returns the container's snapshot directory, where its
// upper disk goes, and the cache of lower images. Both follow the layout of
// containerd's overlayfs snapshotter, <root>/snapshots/<id>/fs, and anything
// else is refused rather than written into.
func overlayBlockDirs(upperdir, cacheDir string) (string, string, error) {
	snapshotDir := filepath.Dir(upperdir)
	snapshots := filepath.Dir(snapshotDir)
	if filepath.Base(upperdir) != "fs" || filepath.Base(snapshots) != "snapshots" {
		return "", "", fmt.Errorf("%s=%s: %s is not an overlayfs snapshot (<root>/snapshots/<id>/fs)",
			annotRootfsMode, RootfsModeOverlayBlock, upperdir)
	}
	if cacheDir == "" {
		cacheDir = filepath.Join(filepath.Dir(snapshots), overlayBlockCacheName)
	}
	return snapshotDir, cacheDir, nil
}

// overlayBlockVolumes sorts the bind mounts of the spec. A directory becomes a
// virtio-fs export. /etc/resolv.conf, /etc/hosts and /etc/hostname become the
// initrd files the guest init copies into the root. Any other file is left
// out with a warning: the guest root is a disk, and there is nothing to bind
// it onto.
func overlayBlockVolumes(mounts []specs.Mount) ([]overlayBlockExport, []types.InitrdFile, error) {
	netFiles := map[string]string{
		"/etc/resolv.conf": "/urunc-resolv.conf",
		"/etc/hosts":       "/urunc-hosts",
		"/etc/hostname":    "/urunc-hostname",
	}
	var exports []overlayBlockExport
	var files []types.InitrdFile
	for _, m := range mounts {
		if !isBindMount(m) {
			continue
		}
		dest := filepath.Clean(m.Destination)
		if !filepath.IsAbs(dest) || dest == "/" || strings.ContainsAny(dest, "\t\n") {
			return nil, nil, fmt.Errorf("%s=%s: cannot mount a volume at %q", annotRootfsMode, RootfsModeOverlayBlock, m.Destination)
		}
		var st unix.Stat_t
		if err := unix.Stat(m.Source, &st); err != nil {
			return nil, nil, fmt.Errorf("volume %s: %w", m.Source, err)
		}
		switch st.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			n := len(exports)
			exports = append(exports, overlayBlockExport{
				tag:    "share" + strconv.Itoa(n),
				source: m.Source,
				share:  overlayBlockShareDir + "/" + strconv.Itoa(n),
				dest:   dest,
				ro:     hasOption(m.Options, "ro"),
				owner:  bindMountOwner{uid: st.Uid, gid: st.Gid},
			})
		case unix.S_IFREG:
			name, ok := netFiles[dest]
			if !ok {
				uniklog.Warnf("%s=%s: the file volume %s at %s is left out", annotRootfsMode, RootfsModeOverlayBlock, m.Source, dest)
				continue
			}
			data, err := os.ReadFile(m.Source)
			if err != nil {
				return nil, nil, fmt.Errorf("volume %s: %w", m.Source, err)
			}
			files = append(files, types.InitrdFile{Name: name, Data: string(data)})
		default:
			uniklog.Warnf("%s=%s: the volume %s at %s is neither a directory nor a file and is left out",
				annotRootfsMode, RootfsModeOverlayBlock, m.Source, dest)
		}
	}
	return exports, files, nil
}

func hasOption(opts []string, want string) bool {
	for _, o := range opts {
		if o == want {
			return true
		}
	}
	return false
}

// ensureLowerImage returns the read-only ext4 image of the snapshot's image
// layers, building it on first use.
//
// The cache key is the list of layers: committed snapshots do not change, so
// the same list is the same image. The image is built under an flock into a
// temporary file renamed into place, so concurrent containers of one image
// build it once. It has no journal and is read-only on the host too.
func ensureLowerImage(mke2fs, cacheDir string, l overlayLayout) (string, error) {
	sum := sha256.Sum256([]byte(strings.Join(l.lowers, ":")))
	key := hex.EncodeToString(sum[:16])
	disk := filepath.Join(cacheDir, key+".ext4")
	if isRegularFile(disk) {
		return disk, nil
	}
	if err := os.MkdirAll(cacheDir, 0o711); err != nil {
		return "", fmt.Errorf("create the rootfs cache %s: %w", cacheDir, err)
	}
	unlock, err := lockPath(disk + ".lock")
	if err != nil {
		return "", err
	}
	defer unlock()
	if isRegularFile(disk) {
		return disk, nil
	}

	src := l.lowers[0]
	if len(l.lowers) > 1 {
		// Two or more layers with no upper make a read-only overlay. A single
		// layer is a plain directory already.
		src = disk + ".mnt"
		if err := os.MkdirAll(src, 0o700); err != nil {
			return "", fmt.Errorf("create %s: %w", src, err)
		}
		opts := "lowerdir=" + strings.Join(l.lowers, ":")
		if l.userxattr {
			opts += ",userxattr"
		}
		if err := unix.Mount("overlay", src, "overlay", unix.MS_RDONLY, opts); err != nil {
			return "", fmt.Errorf("mount the image layers at %s: %w", src, err)
		}
		defer func() {
			_ = unix.Unmount(src, unix.MNT_DETACH)
			_ = os.Remove(src)
		}()
	}

	sizeMB, inodes, err := lowerImageGeometry(src)
	if err != nil {
		return "", err
	}
	staging := disk + ".tmp"
	_ = os.Remove(staging)
	started := time.Now()
	out, err := exec.Command(mke2fs, "-q", "-t", "ext4", "-d", src, "-L", "rootfs", "-m", "0",
		"-O", "^has_journal", "-N", strconv.FormatInt(inodes, 10), "-E", "root_owner=0:0",
		staging, strconv.Itoa(sizeMB)+"M").CombinedOutput()
	if err != nil {
		_ = os.Remove(staging)
		return "", fmt.Errorf("build the rootfs image %s: %s: %w", disk, strings.TrimSpace(string(out)), err)
	}
	if err := os.Chmod(staging, 0o444); err != nil {
		_ = os.Remove(staging)
		return "", fmt.Errorf("make %s read-only: %w", staging, err)
	}
	if err := os.Rename(staging, disk); err != nil {
		_ = os.Remove(staging)
		return "", fmt.Errorf("install the rootfs image %s: %w", disk, err)
	}
	uniklog.Debugf("built the rootfs image %s (%d MiB apparent) in %s", disk, sizeMB, time.Since(started).Round(time.Millisecond))
	return disk, nil
}

// lowerImageGeometry sizes the read-only image from the tree it holds: every
// file's size rounded up to a block, one block per other entry, plus 20% and
// 64 MiB for the filesystem's own metadata. The file is sparse, so the slack
// costs no host space. The inode count is the entry count plus 20% and 4096.
func lowerImageGeometry(root string) (int, int64, error) {
	var bytes, entries int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries++
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			bytes += (info.Size() + ext4BlockSize - 1) / ext4BlockSize * ext4BlockSize
		} else {
			bytes += ext4BlockSize
		}
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("size the image layers at %s: %w", root, err)
	}
	total := bytes + bytes/5 + 64<<20
	return int((total + 1<<20 - 1) >> 20), entries + entries/5 + 4096, nil
}

// ensureUpperImage returns the container's writable upper disk, creating an
// empty sparse ext4 on first use and reusing it after. The monitor runs as
// the container user, which has to open it read-write.
func ensureUpperImage(mke2fs, snapshotDir string, uid, gid uint32) (string, error) {
	disk := filepath.Join(snapshotDir, overlayBlockUpperName)
	if _, err := os.Stat(disk); err == nil {
		return disk, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect the upper disk %s: %w", disk, err)
	}
	staging := disk + ".tmp"
	_ = os.Remove(staging)
	out, err := exec.Command(mke2fs, "-q", "-F", "-t", "ext4", "-m", "0", "-L", "upper",
		"-E", "root_owner=0:0", staging, overlayBlockUpperSize).CombinedOutput()
	if err != nil {
		_ = os.Remove(staging)
		return "", fmt.Errorf("create the upper disk %s: %s: %w", disk, strings.TrimSpace(string(out)), err)
	}
	if err := os.Chown(staging, int(uid), int(gid)); err != nil {
		_ = os.Remove(staging)
		return "", fmt.Errorf("give the upper disk to %d:%d: %w", uid, gid, err)
	}
	if err := os.Chmod(staging, 0o600); err != nil {
		_ = os.Remove(staging)
		return "", fmt.Errorf("restrict the upper disk: %w", err)
	}
	if err := os.Rename(staging, disk); err != nil {
		_ = os.Remove(staging)
		return "", fmt.Errorf("install the upper disk %s: %w", disk, err)
	}
	return disk, nil
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// lockPath takes an exclusive flock on path, creating it if needed, and
// returns the function that releases it.
func lockPath(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
