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
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// rootfsSelector encapsulates the context for rootfs selection
type rootfsSelector struct {
	bundle     string
	cntrRootfs string
	annot      map[string]string
	unikernel  types.Unikernel
	vmm        types.VMM
	vfsdPath   string
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

// chooseRootfs determines the best rootfs configuration based on available options
// Priority order:
//  1. Initrd (if specified)
//  2. Explicit block device annotation (if mounted at /)
//  3. Container rootfs as block device (if MountRootfs=true and supported)
//  4. Container rootfs as shared-fs: virtiofs > 9pfs (if MountRootfs=true and supported)
//  5. No rootfs
func chooseRootfs(bundle string, cntrRootfs string, annot map[string]string,
	unikernel types.Unikernel, vmm types.VMM, vfsdPath string) (types.RootfsParams, error) {

	selector := &rootfsSelector{
		bundle:     bundle,
		cntrRootfs: cntrRootfs,
		annot:      annot,
		unikernel:  unikernel,
		vmm:        vmm,
		vfsdPath:   vfsdPath,
	}

	// Priority 1: Initrd
	result, ok := selector.tryInitrd()
	if ok {
		return result, nil
	}

	// Priority 2: Explicit block annotation
	result, ok = selector.tryExplicitBlock()
	if ok {
		return result, nil
	}

	// Priority 3 & 4: Container rootfs (block or shared-fs)
	result, ok = selector.tryContainerRootfs()
	if ok {
		return switchMonRootfs(result, bundle)
	}

	if selector.shouldMountContainerRootfs() {
		return types.RootfsParams{}, fmt.Errorf("can not use the container rootfs as the sandbox's guest rootfs through block or shared-fs")
	}

	uniklog.Info("no rootfs configured for guest")
	result.MonRootfs = cntrRootfs
	return result, nil

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

// nolint:gocyclo
// prepareMonRootfs prepares the rootfs where the monitor will execute. It
// essentially sets up the devices (KVM, snapshotter block device) that are required
// for the guest execution and any other files (e.g. binaries).
func prepareMonRootfs(monRootfs string, monitorPath string, monitorDataPath string, needsKVM bool, needsTAP bool) error {
	err := fileFromHost(monRootfs, monitorPath, "", unix.MS_BIND|unix.MS_PRIVATE, false)
	if err != nil {
		return err
	}

	// TODO: Remove these when we switch to static binaries
	monitorName := filepath.Base(monitorPath)
	if monitorName != "firecracker" {
		err = fileFromHost(monRootfs, "/lib", "", unix.MS_BIND|unix.MS_PRIVATE, false)
		if err != nil {
			return err
		}

		err = fileFromHost(monRootfs, "/lib64", "", unix.MS_BIND|unix.MS_PRIVATE, false)
		if err != nil {
			// If the file does not exist, just ignore it
			if !os.IsNotExist(err) {
				return err
			}
		}

		err = fileFromHost(monRootfs, "/usr/lib", "", unix.MS_BIND|unix.MS_PRIVATE, false)
		if err != nil {
			return err
		}
	}

	// TODO: Remove these when we switch to static binaries
	if len(monitorName) >= 4 && monitorName[:4] == "qemu" {
		var qDataPath string
		var sBiosPath string
		var err error
		if monitorDataPath == "" {
			qDataPath, err = findQemuDataDir("qemu")
		} else {
			qDataPath = filepath.Join(monitorDataPath, "qemu")
			err = nil
		}
		if err != nil {
			return err
		}

		err = fileFromHost(monRootfs, qDataPath, "/usr/share/qemu", unix.MS_BIND|unix.MS_PRIVATE, false)
		if err != nil {
			return err
		}

		if monitorDataPath == "" {
			sBiosPath, err = findQemuDataDir("seabios")
		} else {
			sBiosPath = filepath.Join(monitorDataPath, "seabios")
			err = nil
		}
		if err != nil {
			return fmt.Errorf("failed to get info of seabios directory: %w", err)
		}
		err = fileFromHost(monRootfs, sBiosPath, "/usr/share/seabios", unix.MS_BIND|unix.MS_PRIVATE, false)
		if err != nil {
			// In urunc-deploy and in some distros seabios does not exist and
			// we do not need it. So if we could not find it, just ignore it.
			if !os.IsNotExist(err) {
				return err
			}
		}
	}

	newProcDir := filepath.Join(monRootfs, "/proc")
	err = os.MkdirAll(newProcDir, 0555)
	if err != nil {
		return err
	}

	err = unix.Mount("proc", newProcDir, "proc", 0, "")
	if err != nil {
		return err
	}

	err = createTmpfs(monRootfs, "/dev", unix.MS_NOSUID|unix.MS_STRICTATIME, "755", "65536k")
	if err != nil {
		return err
	}

	err = setupDev(monRootfs, "/dev/null")
	if err != nil {
		return err
	}

	err = setupDev(monRootfs, "/dev/urandom")
	if err != nil {
		return err
	}

	if needsTAP {
		err = setupDev(monRootfs, "/dev/net/tun")
		if err != nil {
			return err
		}
	}

	if needsKVM {
		err = setupDev(monRootfs, "/dev/kvm")
		if err != nil {
			return err
		}
	}

	// Setup /dev/pts for PTY support (needed for console and debugging tools like cntr)
	// This allows tools like cntr to attach to the container with a shell
	devPtsDir := filepath.Join(monRootfs, "/dev/pts")
	err = os.MkdirAll(devPtsDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create /dev/pts directory: %w", err)
	}

	// Mount devpts filesystem
	// Using newinstance creates an isolated pts namespace for this container
	err = unix.Mount("devpts", devPtsDir, "devpts", unix.MS_NOSUID|unix.MS_NOEXEC, "newinstance,ptmxmode=0666,mode=0620,gid=5")
	if err != nil {
		return fmt.Errorf("failed to mount devpts: %w", err)
	}

	// Create /dev/ptmx as a symlink to /dev/pts/ptmx
	// This is the standard way to provide the PTY master device
	ptmxPath := filepath.Join(monRootfs, "/dev/ptmx")
	err = os.Symlink("pts/ptmx", ptmxPath)
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
