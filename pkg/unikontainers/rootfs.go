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
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// TODO: Find and set the correct size for the tmpfs in the host
const tmpfsSizeForNoRootfs = "65536k"

type rootfsBuilder interface {
	preSetup() error
	postSetup() error
	getMounts() ([]specs.Mount, error)
	getBlockDevs() ([]types.BlockDevParams, error)
	getSharedDirs() (types.SharedfsParams, error)
	preStartCmd() []string
}

// tmpfsMount creates a mount for a tmpfs in the form of "/tmp" at target
func tmpfsMount(target string, size string) specs.Mount {
	return specs.Mount{
		Type:        "tmpfs",
		Source:      "tmpfs",
		Destination: target,
		Options: []string{
			"nosuid",
			"noexec",
			"strictatime",
			"mode=1777",
			"size=" + size,
			"private",
		},
	}
}

// bindMount builds a non-recursive, and private if argument is set, bind mount
// of source at target
func bindMount(source string, target string, private bool) specs.Mount {
	m := specs.Mount{
		Type:        "bind",
		Source:      source,
		Destination: target,
		Options:     []string{"bind"},
	}

	if private {
		m.Options = append(m.Options, "private")
	}

	return m
}

// devPermBits are the permission bits added to every device node the monitor
// gets. Widening to others lets a monitor running as a non-root user open the
// node, without urunc having to resolve the kvm/disk group of the host.
const devPermBits os.FileMode = 0o006

// monitorDeviceMode returns the permission bits to give the monitor's copy of a
// host device node, widened so any user can read/write it.
func monitorDeviceMode(hostMode os.FileMode) os.FileMode {
	return (hostMode & 0o777) | devPermBits
}

// deviceFromHost finds a device in the host from the path and returns its info
// in the form of specs.LinuxDevice. It also widens the FileMode of the device
// so non-root users can access the devices.
func deviceFromHost(path string) (specs.LinuxDevice, error) {
	var devStat unix.Stat_t
	err := unix.Stat(path, &devStat)
	if err != nil {
		return specs.LinuxDevice{}, err
	}

	var devType string
	switch devStat.Mode & unix.S_IFMT {
	case unix.S_IFCHR:
		devType = "c"
	case unix.S_IFBLK:
		devType = "b"
	default:
		return specs.LinuxDevice{}, fmt.Errorf("%s is not a device node", path)
	}

	// TODO: We should make this a configuration option so users can choose if it
	// the permission bits should widen.
	mode := monitorDeviceMode(os.FileMode(devStat.Mode))
	uid := devStat.Uid
	gid := devStat.Gid

	return specs.LinuxDevice{
		Path:     path,
		Type:     devType,
		Major:    int64(unix.Major(uint64(devStat.Rdev))),
		Minor:    int64(unix.Minor(uint64(devStat.Rdev))),
		FileMode: &mode,
		UID:      &uid,
		GID:      &gid,
	}, nil
}

// rootfsSelector encapsulates the context for rootfs selection
type rootfsSelector struct {
	bundle     string
	cntrRootfs string
	annot      map[string]string
	unikernel  types.Unikernel
	vmm        types.VMM
	vfsdPath   string
}

type noRootfs struct {
	monRootfs            string
	annotBlockPath       string
	annotBlockMountPoint string
}

func (n noRootfs) preSetup() error {
	return nil
}

func (n noRootfs) postSetup() error {
	return nil
}

func (n noRootfs) getMounts() ([]specs.Mount, error) {
	return []specs.Mount{tmpfsMount("/tmp", tmpfsSizeForNoRootfs)}, nil
}

func (n noRootfs) getBlockDevs() ([]types.BlockDevParams, error) {
	blkImgs := []types.BlockDevParams{}
	blockFromAnnot, err := handleExplicitBlockImage(n.annotBlockPath,
		n.annotBlockMountPoint)
	if err != nil {
		return nil, err
	}

	if blockFromAnnot.Source != "" && blockFromAnnot.MountPoint != "/" {
		// TODO: Add proper support for multiple block Images from the container's
		// image. This requires adding more annotations too.
		blockFromAnnot.ID = "annot_vol"
		blkImgs = append(blkImgs, blockFromAnnot)
	}

	return blkImgs, nil
}

// TODO: Return an array instead of a single struct
func (n noRootfs) getSharedDirs() (types.SharedfsParams, error) {
	return types.SharedfsParams{}, nil
}

func (n noRootfs) preStartCmd() []string {
	return nil
}

// newRootfsResult creates a RootfsParams with common defaults
func newRootfsResult(rootfsType string, path string, mountedPath string, monRootfs string) types.RootfsParams {
	return types.RootfsParams{
		Type:        rootfsType,
		Path:        path,
		MountedPath: mountedPath,
		MonRootfs:   monRootfs,
	}
}

// tryInitrd checks for initrd-based rootfs based on annotation values
func (rs *rootfsSelector) tryInitrd() (types.RootfsParams, bool) {
	initrdPath := rs.annot[annotInitrd]
	if initrdPath == "" {
		return types.RootfsParams{}, false
	}

	return newRootfsResult("initrd", initrdPath, "", rs.cntrRootfs), true
}

// tryExplicitBlock checks for explicit block device annotation with
// a mountpoint at "/"
func (rs *rootfsSelector) tryExplicitBlock() (types.RootfsParams, bool) {
	blockPath := rs.annot[annotBlock]
	blockMntPoint := rs.annot[annotBlockMntPoint]

	// Only use explicit block if it's meant to be root (mounted at /)
	if blockPath == "" || blockMntPoint != "/" || !rs.unikernel.SupportsBlock() {
		return types.RootfsParams{}, false
	}

	return newRootfsResult("block", blockPath, "", rs.cntrRootfs), true
}

// shouldMountContainerRootfs checks if container rootfs should be mounted
// based on the respective annotation
func (rs *rootfsSelector) shouldMountContainerRootfs() bool {
	annotValue := rs.annot[annotMountRootfs]
	if annotValue == "" {
		return false
	}

	shouldMount, err := strconv.ParseBool(annotValue)
	if err != nil {
		uniklog.Warnf("Invalid value in MountRootfs annotation: %s. Urunc will not mount any rootfs to the guest.", annotValue)
		return false
	}

	return shouldMount
}

// tryContainerBlockRootfs checks if container rootfs can be used as a block device
// for guest's rootfs
func (rs *rootfsSelector) tryContainerBlockRootfs() (types.RootfsParams, bool) {
	if !rs.unikernel.SupportsBlock() {
		return types.RootfsParams{}, false
	}

	rootFsDevice, err := getMountInfo(rs.cntrRootfs)
	if err != nil {
		uniklog.Errorf("failed to get container's rootfs mount info: %v", err)
		return types.RootfsParams{}, false
	}

	if !rs.unikernel.SupportsFS(rootFsDevice.FsType) {
		return types.RootfsParams{}, false
	}

	return newRootfsResult("block", rootFsDevice.Source, rs.cntrRootfs, rs.cntrRootfs), true
}

// tryVirtiofs checks if virtiofs can be used
func (rs *rootfsSelector) tryVirtiofs() (types.RootfsParams, bool) {
	if !rs.unikernel.SupportsFS("virtiofs") {
		return types.RootfsParams{}, false
	}

	if !rs.vmm.SupportsSharedfs("virtio") {
		return types.RootfsParams{}, false
	}

	if !fileExists(rs.vfsdPath) {
		return types.RootfsParams{}, false
	}

	return newRootfsResult("virtiofs", rs.cntrRootfs, rs.cntrRootfs, rs.cntrRootfs), true
}

// try9pfs checks if 9pfs can be used
func (rs *rootfsSelector) try9pfs() (types.RootfsParams, bool) {
	if !rs.unikernel.SupportsFS("9pfs") {
		return types.RootfsParams{}, false
	}

	if !rs.vmm.SupportsSharedfs("9p") {
		return types.RootfsParams{}, false
	}

	return newRootfsResult("9pfs", rs.cntrRootfs, rs.cntrRootfs, rs.cntrRootfs), true
}

// tryContainerSharedFS tries shared filesystem options (virtiofs, then 9pfs)
func (rs *rootfsSelector) tryContainerSharedFS() (types.RootfsParams, bool) {
	// Try virtiofs first (preferred)
	result, ok := rs.tryVirtiofs()
	if ok {
		return result, true
	}

	// Fallback to 9pfs
	result, ok = rs.try9pfs()
	if ok {
		return result, true
	}

	return types.RootfsParams{}, false
}

// tryContainerRootfs tries to use container rootfs as a rootfs for the guest
// trying first using it as block device and if not possible as a shared-fs
func (rs *rootfsSelector) tryContainerRootfs() (types.RootfsParams, bool) {
	if !rs.shouldMountContainerRootfs() {
		return types.RootfsParams{}, false
	}

	// Try block-based rootfs first
	result, ok := rs.tryContainerBlockRootfs()
	if ok {
		return result, true
	}

	// Fallback to shared fs
	result, ok = rs.tryContainerSharedFS()
	if ok {
		return result, true
	}

	uniklog.Error("can not use the container rootfs as the sandbox's guest rootfs through block or shared-fs")
	return types.RootfsParams{}, false
}

func switchMonRootfs(res types.RootfsParams, bundle string) (types.RootfsParams, error) {
	monRootfs := filepath.Join(bundle, monitorRootfsDirName)
	err := os.MkdirAll(monRootfs, 0o755)
	if err != nil {
		return types.RootfsParams{}, fmt.Errorf("failed to create monitor rootfs directory %s: %w", monRootfs, err)
	}
	res.MonRootfs = monRootfs

	return res, nil
}

// pivotRootfs changes rootfs with pivot
// It should be called with CWD being the new rootfs
func pivotRootfs(newRoot string) error {
	// Set up directory of previous rootfs
	oldRoot := filepath.Join(newRoot, "/old_root")
	err := os.MkdirAll(oldRoot, 0755)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %w", oldRoot, err)
	}

	err = unix.PivotRoot(".", "old_root")
	if err != nil {
		return fmt.Errorf("failed to pivot root: %w", err)
	}

	// Make sure we are in the new rootfs
	err = os.Chdir("/")
	if err != nil {
		return fmt.Errorf("failed to set CWD as /: %w", err)
	}

	// Make oldroot rslave to make sure our unmounts don't propagate to the
	// host (and thus bork the machine). We don't use rprivate because this is
	// known to cause issues due to races where we still have a reference to a
	// mount while a process in the host namespace are trying to operate on
	// something they think has no mounts (devicemapper in particular).
	err = unix.Mount("", "old_root", "", unix.MS_SLAVE|unix.MS_REC, "")
	if err != nil {
		return fmt.Errorf("failed to make old_root rslave: %w", err)
	}

	// Perform the unmount. MNT_DETACH allows us to unmount /proc/self/cwd.
	err = unix.Unmount("old_root", unix.MNT_DETACH)
	if err != nil {
		return fmt.Errorf("failed to unmount old_root: %w", err)
	}

	// We no longer need the old rootfs
	err = os.RemoveAll("old_root")
	if err != nil {
		return fmt.Errorf("failed to remove old_root: %w", err)
	}

	return nil
}

// changeRoot changes the rootfs to rootfsDir. If pivot is true, then we will
// use pivot (requires mount namespaces), otherwise we will use chroot
func changeRoot(rootfsDir string, pivot bool) error {
	// Set CWD the rootfs of the container
	err := os.Chdir(rootfsDir)
	if err != nil {
		return err
	}

	if pivot {
		err = pivotRootfs(rootfsDir)
		if err != nil {
			return err
		}
	} else {
		err = unix.Chroot(".")
		if err != nil {
			return err
		}
	}

	// Set CWD the rootfs of the container to ensure we are in the new rootfs
	err = os.Chdir("/")
	if err != nil {
		return err
	}

	return nil
}

// getMonitorDevices returns the devices which are needed for the execution of
// the monitor process
func getMonitorDevices(needsKVM bool) ([]specs.LinuxDevice, error) {
	nullDev, err := deviceFromHost("/dev/null")
	if err != nil {
		return nil, fmt.Errorf("could not get host device /dev/null: %w", err)
	}
	randomDev, err := deviceFromHost("/dev/urandom")
	if err != nil {
		return nil, fmt.Errorf("could not get host device /dev/urandom: %w", err)
	}
	devices := []specs.LinuxDevice{nullDev, randomDev}
	// The tun device is always included because in urunc create we can not know
	// if there will be a virtual ethernet device or not, since the CNI hooks
	// have not executed yet. Therefore, the decision about whether it is actually
	// created in the monitor rootfs is decided in Exec, which checks the
	// network configuration. However, if the host does not have the "/dev/net/tun"
	// device then simply skip it and fail later if the container requires network
	tunDev, err := deviceFromHost("/dev/net/tun")
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			uniklog.Warnf("could not find /dev/net/tun in host, skipping it")
		} else {
			return nil, fmt.Errorf("could not get host device /dev/net/tun: %w", err)
		}
	} else {
		devices = append(devices, tunDev)
	}

	if needsKVM {
		kvmDev, err := deviceFromHost("/dev/kvm")
		if err != nil {
			return nil, fmt.Errorf("could not get host device /dev/kvm: %w", err)
		}
		devices = append(devices, kvmDev)
	}

	return devices, nil
}

// setupConsole setups a /dev/console file for the monitor's execution environment
func setupConsole(monRootfs string) error {
	// Create /dev/ptmx as a symlink to /dev/pts/ptmx
	// This is the standard way to provide the PTY master device
	ptmxPath := filepath.Join(monRootfs, "/dev/ptmx")
	err := os.Symlink("pts/ptmx", ptmxPath)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to create /dev/ptmx symlink: %w", err)
	}

	// Create /dev/console file
	consolePath := filepath.Join(monRootfs, "/dev/console")
	consoleFile, err := os.OpenFile(consolePath, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return fmt.Errorf("failed to create /dev/console: %w", err)
	}
	defer consoleFile.Close()

	// Ensure correct permissions
	if err := consoleFile.Chmod(0o666); err != nil {
		return fmt.Errorf("failed to chmod /dev/console: %w", err)
	}

	return nil
}

// mountsForMonitor returns all the required mounts from the host for the
// monitor execution: the binary, supporting libraries and data directories.
// These are the host files that need to be mirrored inside the monitor rootfs.
// In addition, it also includes the special filesystems required for the monitor
// execution (e.g. procfs)
func mountsForMonitor(monitorPath string, monitorDataPath string) ([]specs.Mount, error) {
	procMount := specs.Mount{
		Type:        "proc",
		Source:      "proc",
		Destination: "/proc",
	}
	devMount := specs.Mount{
		Type:        "tmpfs",
		Source:      "tmpfs",
		Destination: "/dev",
		Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k", "private"},
	}
	devPtsMount := specs.Mount{
		Type:        "devpts",
		Source:      "devpts",
		Destination: "/dev/pts",
		Options:     []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"},
	}

	mounts := []specs.Mount{procMount, devMount, devPtsMount, bindMount(monitorPath, monitorPath, true)}

	monitorName := filepath.Base(monitorPath)
	// TODO: Remove most of these when we switch to static binaries.
	if monitorName != "firecracker" {
		mounts = append(mounts, bindMount("/lib", "/lib", true))

		// If /lib64 does not exist, just ignore it
		if _, err := os.Stat("/lib64"); err == nil {
			mounts = append(mounts, bindMount("/lib64", "/lib64", true))
		} else if !os.IsNotExist(err) {
			return nil, err
		}

		mounts = append(mounts, bindMount("/usr/lib", "/usr/lib", true))
	}

	if len(monitorName) >= 4 && monitorName[:4] == "qemu" {
		qDataPath := filepath.Join(monitorDataPath, "qemu")
		sBiosPath := filepath.Join(monitorDataPath, "seabios")
		if monitorDataPath == "" {
			var err error
			qDataPath, err = findQemuDataDir("qemu")
			if err != nil {
				return nil, err
			}
			sBiosPath, err = findQemuDataDir("seabios")
			if err != nil {
				return nil, fmt.Errorf("failed to get info of seabios directory: %w", err)
			}
		}

		mounts = append(mounts, bindMount(qDataPath, "/usr/share/qemu", true))

		// In urunc-deploy and in some distros seabios does not exist and
		// we do not need it. So if we could not find it, just ignore it.
		if _, err := os.Stat(sBiosPath); err == nil {
			mounts = append(mounts, bindMount(sBiosPath, "/usr/share/seabios", true))
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}

	return mounts, nil
}
