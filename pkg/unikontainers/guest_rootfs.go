// Copyright (c) 2023-2025, Nubificus LTD
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

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// rootfsSelector encapsulates the context for rootfs selection
type rootfsSelector struct {
	bundle     string
	cntrRootfs string
	annot     map[string]string
	unikernel  types.Unikernel
	vmm        types.VMM
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

	rootFsDevice, err := getBlockDevice(rs.cntrRootfs)
	if err != nil {
		uniklog.Errorf("failed to get container's rootfs mount info: %v", err)
		return types.RootfsParams{}, false
	}

	if !rs.unikernel.SupportsFS(rootFsDevice.FsType) {
		return types.RootfsParams{}, false
	}

	return newRootfsResult("block", rootFsDevice.Image, rs.cntrRootfs, rs.cntrRootfs), true
}

// tryVirtiofs checks if virtiofs can be used
func (rs *rootfsSelector) tryVirtiofs() (types.RootfsParams, bool) {
	if !rs.unikernel.SupportsFS("virtiofs") {
		return types.RootfsParams{}, false
	}

	if !rs.vmm.SupportsSharedfs("virtio") {
		return types.RootfsParams{}, false
	}

	if !fileExists(virtiofsHostBinPath) {
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

	uniklog.Error("can not use the container rootfs as block, or through shared-fs")
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
	unikernel types.Unikernel, vmm types.VMM) (types.RootfsParams, error) {

	selector := &rootfsSelector{
		bundle:     bundle,
		cntrRootfs: cntrRootfs,
		annot:     annot,
		unikernel:  unikernel,
		vmm:        vmm,
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
		return types.RootfsParams{}, fmt.Errorf("can not mount container's rootfs as block or through shared-fs to guest")
	}

	uniklog.Info("no rootfs configured for guest")
	result.MonRootfs = cntrRootfs
	return result, nil

}
