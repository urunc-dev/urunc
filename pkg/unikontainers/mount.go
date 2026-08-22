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
//
// Parts of this file have been taken from
// https://github.com/opencontainers/runc/blob/8eb2f43047ce24f06a4cbfd9af4aaedab1062bfb/libcontainer/rootfs_linux.go
// which comes with an Apache 2.0 license. For more information check runc's
// licence.

package unikontainers

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"

	"github.com/containerd/containerd/mount"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/moby/sys/userns"
	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
)

var ErrCopyDir = errors.New("can not copy a directory")

var (
	mkdirAllHook        = os.MkdirAll
	osChmodHook         = os.Chmod
	runningInUserNSHook = userns.RunningInUserNS
	statSyscall         = unix.Stat
	statfsSyscall       = unix.Statfs
	mknodSyscall        = unix.Mknod
	chmodSyscall        = unix.Chmod
	chownSyscall        = os.Chown
	mountSyscall        = unix.Mount
	openSyscall         = unix.Open
	closeSyscall        = unix.Close
	secureJoinHook      = securejoin.SecureJoin
	copyFileHook        = copyFile
	containerdMountHook = func(m *mount.Mount, target string) error {
		return m.Mount(target)
	}
)

// setupDevices creates every device from the list inside monitor's rootfs
func setupDevices(monRootfs string, devices []specs.LinuxDevice, needsTAP bool) error {
	for _, dev := range devices {
		if !needsTAP && dev.Path == "/dev/net/tun" {
			continue
		}
		err := setupDev(monRootfs, dev)
		if err != nil {
			return err
		}
	}

	return nil
}

// setupDev set ups one new device in the container's rootfs.
// This function will get the major and minor number of
// the device from the host's rootfs and it will replicate the device
// inside the container's rootfs.
// When running in a user namespace, urunc bind-mounts the host device node
// instead of using mknod.
func setupDev(monRootfs string, dev specs.LinuxDevice) error {
	// In a user namespace, always bind-mount the existing host device node.
	// Only MS_BIND is used here (no extra flags) to mirror runc's device handling.
	if runningInUserNSHook() {
		return applyMount(monRootfs, bindMount(dev.Path, dev.Path, false))
	}

	var devType uint32
	switch dev.Type {
	case "c":
		devType = unix.S_IFCHR
	case "b":
		devType = unix.S_IFBLK
	default:
		return fmt.Errorf("%s is not a device node", dev.Path)
	}

	// Set the correct target path
	relHostPath, err := filepath.Rel("/", dev.Path)
	if err != nil {
		return fmt.Errorf("failed to get relative path of %s to /: %w", dev.Path, err)
	}
	dstPath := filepath.Join(monRootfs, relHostPath)
	// If the device is not at /dev but further down the tree, create
	// the necessary directories
	if filepath.Dir(dev.Path) != "/dev" {
		dstDir := filepath.Dir(dstPath)
		err = mkdirAllHook(dstDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dstDir, err)
		}
	}

	if dev.FileMode == nil {
		return fmt.Errorf("reference of %s FileMode is nil", dev.Path)
	}
	// Create the new device node
	permBits := uint32(*dev.FileMode) & 0o777
	if dev.Major < 0 || dev.Major > math.MaxUint32 ||
		dev.Minor < 0 || dev.Minor > math.MaxUint32 {
		return fmt.Errorf("device number out of range for %s: major=%d minor=%d", dstPath, dev.Major, dev.Minor)
	}
	newDev := unix.Mkdev(uint32(dev.Major), uint32(dev.Minor)) //#nosec G115 -- device numbers validated previously
	if newDev > uint64(math.MaxInt) {
		return fmt.Errorf("device number for %s too large: %d", dstPath, newDev)
	}
	err = mknodSyscall(dstPath, devType|permBits, int(newDev)) //#nosec G115 -- device numbers validated previously
	if err != nil {
		return fmt.Errorf("failed to make device node %s: %w", dstPath, err)
	}

	// Set up permissions, adding rw for others to ensure that any user can
	// read/write them. This is helpful for non-root monitor execution and
	// removes the burdain of getting kvm/block group id
	permBits |= 0o006
	err = chmodSyscall(dstPath, permBits)
	if err != nil {
		return fmt.Errorf("failed to chmod %s: %w", dstPath, err)
	}

	if dev.UID == nil {
		return fmt.Errorf("reference of %s UID is nil", dev.Path)
	}
	if dev.GID == nil {
		return fmt.Errorf("reference of %s GID is nil", dev.Path)
	}
	// Set the owner as in the original file
	err = chownSyscall(dstPath, int(*dev.UID), int(*dev.GID))
	if err != nil {
		return fmt.Errorf("failed to chown %s: %w", dstPath, err)
	}

	return nil
}

// fileFromHost copies a file from the host's rootfs into monRootfs, preserving
// the original file's permissions and ownership. If target is empty, the file
// is placed at the same path it has on the host.
// A copy is preferred for monitor binaries: none of the monitor processes share
// memory with other processes of the same monitor, at the cost of being slower
// and consuming more space. Copying a directory is not supported and returns
// ErrCopyDir.
func fileFromHost(monRootfs string, hostPath string, target string) error {
	// Get the info of the original file
	var fileInfo unix.Stat_t
	err := statSyscall(hostPath, &fileInfo)
	if err != nil {
		return err
	}

	if (fileInfo.Mode & unix.S_IFMT) == unix.S_IFDIR {
		return ErrCopyDir
	}

	if target == "" {
		// Set the correct path
		target, err = filepath.Rel("/", hostPath)
		if err != nil {
			return fmt.Errorf("failed to get relative path of %s to /: %w", hostPath, err)
		}
	}
	dstPath, err := secureJoinHook(monRootfs, target)
	if err != nil {
		return fmt.Errorf("failed to resolve target %s: %w", target, err)
	}

	err = copyFileHook(hostPath, dstPath)
	if err != nil {
		return fmt.Errorf("failed to copy file %s: %w", hostPath, err)
	}

	// Set up the permissions and ownership to match the original file.
	err = chmodSyscall(dstPath, fileInfo.Mode)
	if err != nil {
		return fmt.Errorf("failed to chmod %s: %w", dstPath, err)
	}

	err = chownSyscall(dstPath, int(fileInfo.Uid), int(fileInfo.Gid))
	if err != nil {
		return fmt.Errorf("failed to chown %s: %w", dstPath, err)
	}

	return nil
}

// mapRootfsPropagationFlag retrieves the propagation flags of the rootfs
// from the container's configuration
func mapRootfsPropagationFlag(value string) (int, error) {
	mountPropagationMapping := map[string]int{
		"rprivate":    unix.MS_PRIVATE | unix.MS_REC,
		"private":     unix.MS_PRIVATE,
		"rslave":      unix.MS_SLAVE | unix.MS_REC,
		"slave":       unix.MS_SLAVE,
		"rshared":     unix.MS_SHARED | unix.MS_REC,
		"shared":      unix.MS_SHARED,
		"runbindable": unix.MS_UNBINDABLE | unix.MS_REC,
		"unbindable":  unix.MS_UNBINDABLE,
	}

	propagation, exists := mountPropagationMapping[value]
	if !exists {
		return 0, fmt.Errorf("rootfsPropagation=%s is not supported", value)
	}

	return propagation, nil
}

// mapVFSFlag maps a per-mount VFS mount option to its mount(2) flag bit and
// whether the option clears the flag rather than setting it (e.g. "rw" clears
// the read-only flag set by "ro"). It returns an error for options that are not
// VFS flags.
func mapVFSFlag(value string) (flag uintptr, clear bool, err error) {
	vfsFlagMapping := map[string]struct {
		clear bool
		flag  uintptr
	}{
		"ro":            {false, unix.MS_RDONLY},
		"rw":            {true, unix.MS_RDONLY},
		"nosuid":        {false, unix.MS_NOSUID},
		"suid":          {true, unix.MS_NOSUID},
		"nodev":         {false, unix.MS_NODEV},
		"dev":           {true, unix.MS_NODEV},
		"noexec":        {false, unix.MS_NOEXEC},
		"exec":          {true, unix.MS_NOEXEC},
		"noatime":       {false, unix.MS_NOATIME},
		"atime":         {true, unix.MS_NOATIME},
		"nodiratime":    {false, unix.MS_NODIRATIME},
		"diratime":      {true, unix.MS_NODIRATIME},
		"relatime":      {false, unix.MS_RELATIME},
		"norelatime":    {true, unix.MS_RELATIME},
		"strictatime":   {false, unix.MS_STRICTATIME},
		"nostrictatime": {true, unix.MS_STRICTATIME},
		"nosymfollow":   {false, unix.MS_NOSYMFOLLOW},
		"symfollow":     {true, unix.MS_NOSYMFOLLOW},
		"sync":          {false, unix.MS_SYNCHRONOUS},
		"async":         {true, unix.MS_SYNCHRONOUS},
		"dirsync":       {false, unix.MS_DIRSYNC},
		"mand":          {false, unix.MS_MANDLOCK},
		"nomand":        {true, unix.MS_MANDLOCK},
		"silent":        {false, unix.MS_SILENT},
		"loud":          {true, unix.MS_SILENT},
		"lazytime":      {false, unix.MS_LAZYTIME},
		"nolazytime":    {true, unix.MS_LAZYTIME},
		"iversion":      {false, unix.MS_I_VERSION},
		"noiversion":    {true, unix.MS_I_VERSION},
	}

	f, exists := vfsFlagMapping[value]
	if !exists {
		return 0, false, fmt.Errorf("%s is not a supported vfs mount flag", value)
	}

	return f.flag, f.clear, nil
}

// Get the set of mount flags that are set on the mount that contains the given
// path and are locked by CL_UNPRIVILEGED. This is necessary to ensure that
// bind-mounting "with options" will not fail with user namespaces, due to
// kernel restrictions that require user namespace mounts to preserve
// CL_UNPRIVILEGED locked flags.
// Similar to
// https://github.com/opencontainers/umoci/blob/f5d1219acaf67127ebacf6306776d3ff465735ea/oci/config/convert/utils_linux.go#L33
// but returns the uintptr to use in mount later.
func getUnprivilegedMountFlags(path string) (uintptr, error) {
	var st unix.Statfs_t
	err := statfsSyscall(path, &st)
	if err != nil {
		return 0, err
	}

	// statfs reports ST_* flags. They match the corresponding MS_* values for
	// most flags, but ST_RELATIME (0x1000) differs from MS_RELATIME (0x200000),
	// so map each ST_* bit that statfs reports to the MS_* flag used for the
	// remount explicitly.
	lockedFlags := []struct {
		st, ms uintptr
	}{
		{unix.ST_RDONLY, unix.MS_RDONLY},
		{unix.ST_NODEV, unix.MS_NODEV},
		{unix.ST_NOEXEC, unix.MS_NOEXEC},
		{unix.ST_NOSUID, unix.MS_NOSUID},
		{unix.ST_NOATIME, unix.MS_NOATIME},
		{unix.ST_RELATIME, unix.MS_RELATIME},
		{unix.ST_NODIRATIME, unix.MS_NODIRATIME},
	}

	var flags uintptr
	for _, f := range lockedFlags {
		if uintptr(st.Flags)&f.st == f.st {
			flags |= f.ms
		}
	}

	// With neither noatime nor relatime set, the source uses strictatime;
	// preserve it explicitly so a rootless remount of a strictatime-locked source
	// is not rejected with EPERM.
	if flags&(unix.MS_NOATIME|unix.MS_RELATIME) == 0 {
		flags |= unix.MS_STRICTATIME
	}

	return flags, nil
}

// rootfsParentMountPrivate ensures rootfs parent mount is private.
// This is needed for two reasons:
//   - pivot_root() will fail if parent mount is shared;
//   - when we bind mount rootfs, if its parent is not private, the new mount
//     will propagate (leak!) to parent namespace and we don't want that.
func rootfsParentMountPrivate(path string) error {
	var err error
	// Assuming path is absolute and clean.
	// Any error other than EINVAL means we failed,
	// and EINVAL means this is not a mount point, so traverse up until we
	// find one.
	for {
		err = mountSyscall("", path, "", unix.MS_PRIVATE, "")
		if err == nil {
			return nil
		}
		if err != unix.EINVAL || path == "/" {
			break
		}
		path = filepath.Dir(path)
	}

	return fmt.Errorf("could not remount as private the parent mount of %s", path)
}

// prepareRoot prepares the directory of the container's rootfs to safely pivot
// chroot to it.
func prepareRoot(path string, rootfsPropagation string) error {
	flag := unix.MS_SLAVE | unix.MS_REC
	if rootfsPropagation != "" {
		var err error

		flag, err = mapRootfsPropagationFlag(rootfsPropagation)
		if err != nil {
			return err
		}
	}

	err := mountSyscall("", "/", "", uintptr(flag), "")
	if err != nil {
		return err
	}

	err = rootfsParentMountPrivate(path)
	if err != nil {
		return err
	}

	return mountSyscall(path, path, "bind", unix.MS_BIND|unix.MS_REC, "")
}

// applyMounts sets up every mount specified in the mounts argument inside the
// directory of rootfsPath.
func applyMounts(rootfsPath string, mounts []specs.Mount) error {
	for _, m := range mounts {
		err := applyMount(rootfsPath, m)
		if err != nil {
			// NOTE: We do not cleanup here because we assume that
			// a mount namespace was declared and we are inside one.
			// Therefore, after a fail, we will exit reexec (the only
			// process inside the mount namespace and therefore the
			// namespace will get removed and the kernel will cleanup
			// the mounts. Revisit this in the future and check if it
			// is urunc's responsibility to cleanup. If it is we need
			// to account for cases where a mount namespace was not
			// specified and therefore we need to manually unmount everything.
			return fmt.Errorf("failed to apply mount %s: %w", m.Source, err)
		}
	}

	return nil
}

// applyMount mounts a single entry under rootfsPath.
func applyMount(rootfsPath string, m specs.Mount) error {
	target, err := secureJoinHook(rootfsPath, m.Destination)
	if err != nil {
		return fmt.Errorf("failed to resolve mount target %s: %w", m.Destination, err)
	}

	err = createMountPoint(target, m)
	if err != nil {
		return fmt.Errorf("failed to create mount target %s: %w", target, err)
	}

	// containerd's parser cannot translate propagation tokens, and the kernel
	// ignores per-mount VFS flags (nosuid, nodev, ...) when a bind is created, so
	// parse the options once and apply them manually later.
	containerdOpts, propagation, vfsFlags := splitMountOptions(m.Options, m.Type == "bind")

	cm := mount.Mount{
		Type:    m.Type,
		Source:  m.Source,
		Options: containerdOpts,
	}
	err = containerdMountHook(&cm, target)
	if err != nil {
		return fmt.Errorf("failed to mount %s at %s: %w", cm.Source, target, err)
	}

	// The kernel ignores per-mount VFS flags when a bind is created, so re-apply
	// them with a remount. In a user namespace the remount must preserve the
	// flags locked on the source mount, otherwise the kernel rejects it with
	// EPERM (this is what containerd's own bind remount does for us elsewhere).
	if m.Type == "bind" && vfsFlags != 0 {
		remount := vfsFlags | unix.MS_BIND | unix.MS_REMOUNT
		if runningInUserNSHook() {
			locked, err := getUnprivilegedMountFlags(m.Source)
			if err != nil {
				return fmt.Errorf("failed to get locked mount flags of %s: %w", m.Source, err)
			}
			// Merging the locked flags means a source whose mount is locked
			// read-only forces the bind read-only even if the spec asked for rw.
			// We accept that as a trade-off to keep rootless remounts working;
			// runc instead errors out in this case.
			remount |= locked
		}
		err = mountSyscall("", target, "", remount, "")
		if err != nil {
			return fmt.Errorf("failed to apply mount flags for %s: %w", target, err)
		}
	}

	for _, pFlag := range propagation {
		err = mountSyscall("", target, "", uintptr(pFlag), "")
		if err != nil {
			return fmt.Errorf("failed to set propagation flag for %s: %w", m.Destination, err)
		}
	}

	if m.Type == "tmpfs" && slices.Contains(m.Options, "mode=1777") {
		err = osChmodHook(target, 0o777|os.ModeSticky)
		if err != nil {
			return fmt.Errorf("failed to chmod %s: %w", target, err)
		}
	}

	return nil
}

// createMountPoint creates the mountpoint of a mount.
func createMountPoint(target string, m specs.Mount) error {
	if m.Type == "bind" {
		var st unix.Stat_t
		err := statSyscall(m.Source, &st)
		if err != nil {
			return fmt.Errorf("failed to stat mount source %s: %w", m.Source, err)
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			dstDir := filepath.Dir(target)
			err := mkdirAllHook(dstDir, 0755)
			if err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dstDir, err)
			}
			fd, err := openSyscall(target, unix.O_CREAT, 0644)
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", target, err)
			}
			err = closeSyscall(fd)
			if err != nil {
				return fmt.Errorf("failed to close file %s: %w", target, err)
			}

			return nil
		}
	}

	err := mkdirAllHook(target, 0755)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %w", target, err)
	}

	return nil
}

// splitMountOptions splits a mount's options into three groups: the options
// for containerd's mount package, the propagation flags, and the per-mount VFS
// flags. Propagation flags never go to containerd because it cannot parse them
// and would end up in fs-specific data. The VFS flags are where bind mounts
// differ. The kernel ignores per-mount VFS flags when a bind mount is created,
// so with isBind set these options are withheld from containerd and returned
// as flag bits for the caller to apply. For non-bind mounts the VFS options
// stay in the containerd options, since the initial mount(2) applies them; the
// returned flag bits are meaningless in that case and must be ignored.
func splitMountOptions(options []string, isBind bool) ([]string, []int, uintptr) {
	var containerdOpts []string
	var propagation []int
	var vfsFlags uintptr
	for _, o := range options {
		pFlag, err := mapRootfsPropagationFlag(o)
		if err == nil {
			propagation = append(propagation, pFlag)
			continue
		}

		flag, clear, err := mapVFSFlag(o)
		if err == nil {
			if clear {
				vfsFlags &^= flag
			} else {
				vfsFlags |= flag
			}
			// For bind mounts keep the VFS flags separate from containerd
			// and apply them later.
			if isBind {
				continue
			}
		}

		containerdOpts = append(containerdOpts, o)
	}

	return containerdOpts, propagation, vfsFlags
}
