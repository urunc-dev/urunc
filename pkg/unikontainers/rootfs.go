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

// Platform-neutral guest-rootfs selection. Given an OCI bundle and its urunc
// annotations, ChooseRootfs decides how the container rootfs is presented to
// the guest (initrd, block device, virtiofs or 9pfs) based on what the chosen
// unikernel and monitor support. This decision is identical on every platform,
// so the Linux engine and the darwin runner share it here; only the subsequent
// realization of that decision (mount namespaces on Linux) is platform-specific
// and lives in rootfs_linux.go.

package unikontainers

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// rootfsOverlayBlock is the rootfs type of an overlay root on two disks, a
// read-only lower built from the image and a writable upper.
const rootfsOverlayBlock = "overlay-block"

// rootfsSelector encapsulates the context for rootfs selection
type rootfsSelector struct {
	bundle     string
	cntrRootfs string
	annot      map[string]string
	unikernel  types.Unikernel
	vmm        types.VMM
	vfsdPath   string
}

// newRootfsResult creates a RootfsParams with common defaults.
func newRootfsResult(rootfsType string, path string, mountedPath string) types.RootfsParams {
	return types.RootfsParams{
		Type:        rootfsType,
		Path:        path,
		MountedPath: mountedPath,
	}
}

// chooseRootfs determines the best rootfs configuration based on available options
// Priority order:
//  1. Initrd (if specified)
//  2. Explicit block device annotation (if mounted at /)
//  3. Container rootfs as block device (if MountRootfs=true and supported)
//  4. Container rootfs as shared-fs: virtiofs > 9pfs (if MountRootfs=true and supported)
//  5. No rootfs
func ChooseRootfs(bundle, specRoot string, annot map[string]string, cfg *UruncConfig) (types.RootfsParams, error) {
	bundleDir := filepath.Clean(bundle)
	rootfsDir := filepath.Clean(specRoot)
	rootfsDir, err := resolveAgainstBase(bundleDir, rootfsDir)
	if err != nil {
		uniklog.Errorf("could not resolve rootfs directory %s: %v", rootfsDir, err)
		return types.RootfsParams{}, err
	}

	if cfg == nil {
		return types.RootfsParams{}, fmt.Errorf("urunc config is required for guest rootfs selection")
	}

	unikernelType := annot[annotType]
	unikernel, err := unikernels.New(unikernelType)
	if err != nil {
		return types.RootfsParams{}, err
	}

	vmmType := annot[annotHypervisor]
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(vmmType), cfg.Monitors)
	if err != nil {
		return types.RootfsParams{}, err
	}

	virtiofsdConfig := cfg.ExtraBins["virtiofsd"]

	selector := &rootfsSelector{
		bundle:     bundleDir,
		cntrRootfs: rootfsDir,
		annot:      annot,
		unikernel:  unikernel,
		vmm:        vmm,
		vfsdPath:   virtiofsdConfig.Path,
	}

	// Priority 1: Initrd
	result, ok := selector.tryInitrd()
	if ok {
		return switchMonRootfs(result, bundleDir), nil
	}

	// Priority 2: Explicit block annotation
	result, ok = selector.tryExplicitBlock()
	if ok {
		return switchMonRootfs(result, bundleDir), nil
	}

	// An overlay-block root is asked for by name, so it is used or the
	// container fails, with no fallback to another rootfs.
	if annot[annotRootfsMode] == RootfsModeOverlayBlock {
		result, err = selector.overlayBlock()
		if err != nil {
			return types.RootfsParams{}, err
		}
		return switchMonRootfs(result, bundleDir), nil
	}

	// Priority 3 & 4: Container rootfs (block or shared-fs)
	result, ok = selector.tryContainerRootfs()
	if ok {
		return switchMonRootfs(result, bundleDir), nil
	}

	if selector.shouldMountContainerRootfs() {
		return types.RootfsParams{}, fmt.Errorf("can not use the container rootfs as the sandbox's guest rootfs through block or shared-fs")
	}

	uniklog.Info("no rootfs configured for guest")

	result = newRootfsResult("", "", selector.cntrRootfs)
	return switchMonRootfs(result, bundleDir), nil
}

// tryInitrd checks for initrd-based rootfs based on annotation values
func (rs *rootfsSelector) tryInitrd() (types.RootfsParams, bool) {
	initrdPath := rs.annot[annotInitrd]
	if initrdPath == "" {
		return types.RootfsParams{}, false
	}

	return newRootfsResult("initrd", initrdPath, rs.cntrRootfs), true
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

	return newRootfsResult("block", blockPath, ""), true
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

	result := newRootfsResult("block", rootFsDevice.Source, rs.cntrRootfs)
	// Carry the filesystem so a generic container boot can put rootfstype= on the
	// kernel command line for the boot initrd to mount /dev/vda.
	result.FsType = rootFsDevice.FsType
	return result, true
}

// overlayBlock selects the overlay-block root of a generic container boot.
// Its disks are built from the container rootfs, the overlayfs snapshot
// mounted at cntrRootfs, by the rootfs builder.
func (rs *rootfsSelector) overlayBlock() (types.RootfsParams, error) {
	if !rs.hasContainerBoot() || !rs.shouldMountContainerRootfs() {
		return types.RootfsParams{}, fmt.Errorf("%s=%s needs a generic container boot that mounts the container rootfs",
			annotRootfsMode, RootfsModeOverlayBlock)
	}
	if !rs.unikernel.SupportsBlock() || !rs.unikernel.SupportsFS("ext4") {
		return types.RootfsParams{}, fmt.Errorf("%s=%s needs a guest that mounts an ext4 disk",
			annotRootfsMode, RootfsModeOverlayBlock)
	}
	result := newRootfsResult(rootfsOverlayBlock, "", rs.cntrRootfs)
	result.FsType = "ext4"
	return result, nil
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

	return newRootfsResult("virtiofs", rs.cntrRootfs, rs.cntrRootfs), true
}

// try9pfs checks if 9pfs can be used
func (rs *rootfsSelector) try9pfs() (types.RootfsParams, bool) {
	if !rs.unikernel.SupportsFS("9pfs") {
		return types.RootfsParams{}, false
	}

	if !rs.vmm.SupportsSharedfs("9p") {
		return types.RootfsParams{}, false
	}

	return newRootfsResult("9pfs", rs.cntrRootfs, rs.cntrRootfs), true
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

// hasContainerBoot reports whether the guest is a generic container boot, i.e.
// an unmodified image booted with a host kernel and boot initrd.
func (rs *rootfsSelector) hasContainerBoot() bool {
	return rs.annot[annotBootKernel] != "" && rs.annot[annotBootInitrd] != ""
}

// tryContainerRootfs tries to use container rootfs as a rootfs for the guest
// trying first using it as block device and if not possible as a shared-fs
func (rs *rootfsSelector) tryContainerRootfs() (types.RootfsParams, bool) {
	if !rs.shouldMountContainerRootfs() {
		return types.RootfsParams{}, false
	}

	if rs.hasContainerBoot() {
		// A generic container boot shares the image rootfs into the guest. It
		// prefers a shared filesystem, where the boot initrd mounts the share by
		// the tag the kernel command line names and switch_roots into it. When the
		// monitor has no shared-fs (firecracker), it falls back to the container
		// rootfs as a block device, which requires it to be a real block device
		// (e.g. the devmapper snapshotter, not overlayfs).
		if result, ok := rs.tryContainerSharedFS(); ok {
			return result, true
		}
		if result, ok := rs.tryContainerBlockRootfs(); ok {
			return result, true
		}
		uniklog.Error("can not use the container rootfs as the guest rootfs through shared-fs or block")
		return types.RootfsParams{}, false
	}

	// Try block-based rootfs first.
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

// switchMonRootfs points RootfsParams at the dedicated monitor rootfs, separate
// from the container's image rootfs.
func switchMonRootfs(res types.RootfsParams, bundle string) types.RootfsParams {
	res.MonRootfs = filepath.Join(filepath.Clean(bundle), monitorRootfsDirName)

	return res
}
