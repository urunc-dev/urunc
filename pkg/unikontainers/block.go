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
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/sys/mountinfo"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

// TODO: Find and set the correct size for the tmpfs in the host
const tmpfsSizeForBlockRootfs = "65536k"

var ErrMountpoint = errors.New("no FS is mounted in this mountpoint")

type blockRootfs struct {
	mounts        []specs.Mount
	monRootfs     string
	mountedPath   string
	path          string
	kernelPath    string
	initrdPath    string
	uruncJSONPath string
	guestType     string
	guest         types.Unikernel
}

// getMountInfo determines whether the provided path is a mount point
// by inspecting /proc/self/mountinfo.
// If the path is a mount point, it populates and returns a BlockDevParams struct.
// Otherwise, it returns an error along with an empty BlockDevParams.
// Additionally, when the path is a mount point, getMountInfo verifies
// the mount source to ensure it can use the source as a block device.
// There are cases (e.g. bind mounts) where mounts use the same underlying
// source device as the original mount, so they can appear identical to
// regular mounts when inspecting mount information.
func getMountInfo(path string) (types.BlockDevParams, error) {
	selfProcMountInfo := "/proc/self/mountinfo"

	file, err := os.Open(selfProcMountInfo)
	if err != nil {
		return types.BlockDevParams{}, fmt.Errorf("failed to open mountinfo: %w", err)
	}
	defer file.Close()

	blockDev := types.BlockDevParams{}
	nonSpecialSources := make(map[string]struct{})
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, " - ")
		if len(parts) != 2 {
			return types.BlockDevParams{}, fmt.Errorf("invalid mountinfo line in /proc/self/mountinfo")
		}

		preDash := strings.Fields(parts[0])
		if len(preDash) < 6 {
			continue
		}
		postDash := strings.Fields(parts[1])
		if len(postDash) < 2 {
			continue
		}
		if preDash[4] == path {
			uniklog.WithFields(logrus.Fields{
				"mounted at": path,
				"device":     postDash[1],
				"fstype":     postDash[0],
				"options":    preDash[5],
			}).Debug("Found block device")

			blockDev.Source = postDash[1]
			blockDev.FsType = postDash[0]
			blockDev.MountPoint = path
			// Keep the mount VFS options (field 6 of mountinfo)
			// to restore them later in the delete path.
			blockDev.MountOptions = preDash[5]
			blockDev.ID = ""
			continue
		}
		// Store the source of all mounts with non-special fs
		// (e.g. overlay, tmpfs) in a map
		if postDash[0] != postDash[1] {
			nonSpecialSources[postDash[1]] = struct{}{}
		}
	}

	if blockDev.Source == "" {
		return types.BlockDevParams{}, ErrMountpoint
	}

	// Check if the source of the mountpoint that refers to path
	// exists i the map with the found sources. If this is the case,
	// then we are not dealing with a mount regarding a block device
	// that we can attach to the sandbox.
	_, ok := nonSpecialSources[blockDev.Source]
	if ok {
		return types.BlockDevParams{}, ErrMountpoint
	}

	return blockDev, nil
}

// extractUnikernelFromBlock moves unikernel binary, initrd and urunc.json
// files from old rootfsPath to newRootfsPath
// FIXME: This approach fills up /run with unikernel binaries, initrds and urunc.json
// files for each unikernel we run
func extractBootFiles(rootfsPath string, newRootfsPath string, unikernel string, uruncJSON string, initrd string) error {
	currentUnikernelPath := filepath.Join(rootfsPath, unikernel)
	targetUnikernelPath := filepath.Join(newRootfsPath, unikernel)
	targetUnikernelDir, _ := filepath.Split(targetUnikernelPath)
	err := moveFile(currentUnikernelPath, targetUnikernelDir)
	if err != nil {
		return fmt.Errorf("could not move %s to %s: %w", currentUnikernelPath, targetUnikernelPath, err)
	}

	if initrd != "" {
		currentInitrdPath := filepath.Join(rootfsPath, initrd)
		targetInitrdPath := filepath.Join(newRootfsPath, initrd)
		targetInitrdDir, _ := filepath.Split(targetInitrdPath)
		err = moveFile(currentInitrdPath, targetInitrdDir)
		if err != nil {
			return fmt.Errorf("could not move %s to %s: %w", currentInitrdPath, targetInitrdPath, err)
		}
	}

	currentConfigPath := filepath.Join(rootfsPath, uruncJSON)
	err = moveFile(currentConfigPath, newRootfsPath)
	if err != nil {
		return fmt.Errorf("could not move %s to %s: %w", currentConfigPath, newRootfsPath, err)
	}

	return nil
}

func copyMountfiles(targetPath string, mounts []specs.Mount) error {
	for _, m := range mounts {
		if m.Type != "bind" {
			continue
		}
		err := fileFromHost(targetPath, m.Source, m.Destination)
		if (err != nil) && !errors.Is(err, ErrCopyDir) {
			return err
		}
	}

	return nil
}

func handleExplicitBlockImage(blockImg string, mountPoint string) (types.BlockDevParams, error) {
	if blockImg == "" {
		return types.BlockDevParams{}, nil
	}

	if mountPoint == "" {
		return types.BlockDevParams{}, fmt.Errorf("annotation for block device was set without a mountpoint")
	}

	id := ""
	if mountPoint == "/" {
		id = "rootfs"
	}

	return types.BlockDevParams{
		Source:     blockImg,
		MountPoint: mountPoint,
		ID:         id,
	}, nil
}

// Search all the mount entries in the container's config and
// find the ones that come from a block.
func getBlockVolumes(mounts []specs.Mount, ukernel types.Unikernel) ([]types.BlockDevParams, error) {
	blkImgs := []types.BlockDevParams{}
	for i, m := range mounts {
		// We check only bind mounts
		if m.Type != "bind" {
			continue
		}
		// Get the information of the source path
		// from /proc/self/mountinfo
		mInfo, err := getMountInfo(m.Source)
		if errors.Is(err, ErrMountpoint) {
			// ErrMountpoint means we did not find any
			// such mount and hence we can skip it.
			continue
		}
		if err != nil {
			return nil, err
		}
		if ukernel.SupportsFS(mInfo.FsType) {
			// So, there was an issue which was manifested from the testing.
			// If we have a file (e.g. ext2) and mount it, then we use
			// a loop device for the mount and this is what is shown
			// in the mount list. However, since we perform the unmount
			// the device might also get removed. See
			// https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-block-loop
			// If the device gets removed, then we attach nothing to the
			// sandbox. To resolve this we remove the autoclear flag
			// and therefore the device will persist.
			// NOTE: Although we restore the autoclear flag in the delete path,
			// if delete is never called then the autoclear flag will never
			// get restored.and remounted
			// TODO: Add the above note in a documentation for storage
			// handling
			cleared, err := setLoopAutoclear(mInfo.Source, false)
			if err != nil {
				return nil, err
			}
			mInfo.LoopAutoclear = cleared
			err = unmount(mInfo.MountPoint)
			if err != nil {
				return nil, err
			}
			mInfo.ID = fmt.Sprintf("vol%d", i)
			mInfo.HostMountPoint = mInfo.MountPoint
			mInfo.MountPoint = m.Destination
			blkImgs = append(blkImgs, mInfo)
		}
	}

	return blkImgs, nil
}

// restoreBlockVolumes mounts the block volume sources that were unmounted
// during create
func restoreBlockVolumes(blockArgs []types.BlockDevParams) error {
	for _, b := range blockArgs {
		// Only volumes gathered from the container's mounts carry the
		// host mountpoint where their source was originally mounted.
		if b.HostMountPoint == "" {
			continue
		}
		mounted, err := mountinfo.Mounted(b.HostMountPoint)
		if err != nil {
			return fmt.Errorf("failed to check if %s is a mountpoint: %w", b.HostMountPoint, err)
		}
		if mounted {
			continue
		}

		// restoring block volumes is a simple mount operation, but using
		// containerd can lead to errors because the mount options parser of
		// containerd might not handle some VFS flags correctly and misplace
		// them in the options argument of mount system call.
		// Therefore, do not use containerd for such mounts and handle them
		// directly.
		var flags uintptr
		for _, o := range strings.Split(b.MountOptions, ",") {
			flag, clearFlag, err := mapVFSFlag(o)
			if err != nil {
				continue
			}
			if clearFlag {
				flags &^= flag
			} else {
				flags |= flag
			}
		}
		err = unix.Mount(b.Source, b.HostMountPoint, b.FsType, flags, "")
		if err != nil {
			return fmt.Errorf("failed to remount %s at %s: %w", b.Source, b.HostMountPoint, err)
		}

		if b.LoopAutoclear {
			_, err = setLoopAutoclear(b.Source, true)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// setLoopAutoclear sets or clears the autoclear flag of a loop device and
// returns true when the flag was changed. It returns false without an error
// when the path is not a loop device or the flag already has the wanted value.
func setLoopAutoclear(devPath string, autoclear bool) (bool, error) {
	_, err := os.Stat(filepath.Join("/sys/class/block", filepath.Base(devPath), "loop"))
	if os.IsNotExist(err) {
		// Not a loop device
		return false, nil
	}
	if err != nil {
		return false, err
	}

	dev, err := os.OpenFile(devPath, os.O_RDWR, 0)
	if err != nil {
		return false, fmt.Errorf("failed to open %s: %w", devPath, err)
	}
	defer dev.Close()

	info, err := unix.IoctlLoopGetStatus64(int(dev.Fd()))
	if err != nil {
		return false, fmt.Errorf("failed to get the status of %s: %w", devPath, err)
	}
	if autoclear == (info.Flags&unix.LO_FLAGS_AUTOCLEAR != 0) {
		return false, nil
	}

	if autoclear {
		info.Flags |= unix.LO_FLAGS_AUTOCLEAR
	} else {
		info.Flags &^= unix.LO_FLAGS_AUTOCLEAR
	}
	err = unix.IoctlLoopSetStatus64(int(dev.Fd()), info)
	if err != nil {
		return false, fmt.Errorf("failed to set the status of %s: %w", devPath, err)
	}

	return true, nil
}

// blockDevNodes transforms a list of types.BlockDevParams to a list of
// specs.LinuxDevice which cna be then used for replicating these block
// devices form the host to the monitor's execution environment.
func blockDevNodes(blockArgs []types.BlockDevParams, rootfs types.RootfsParams) ([]specs.LinuxDevice, error) {
	if rootfs.Type != "block" {
		return nil, nil
	}

	var blockDevs []specs.LinuxDevice
	for _, b := range blockArgs {
		// The rootfs device is a real host device only when a container rootfs
		// was converted to a block device (MountedPath set). When it is an
		// explicit block image referenced by an annotation, no node is created.
		if b.Source == rootfs.Path && rootfs.MountedPath == "" {
			continue
		}
		bDev, err := deviceFromHost(b.Source)
		if err != nil {
			return nil, err
		}
		blockDevs = append(blockDevs, bDev)
	}

	return blockDevs, nil
}

func (b blockRootfs) preSetup() error {
	if b.mountedPath == "" {
		return nil
	}

	err := copyMountfiles(b.mountedPath, b.mounts)
	if err != nil {
		return fmt.Errorf("failed to copy files from mount list: %w", err)
	}

	// FIXME: This approach fills up /run with unikernel binaries and
	// urunc.json files for each unikernel instance we run
	err = extractBootFiles(b.mountedPath, b.monRootfs, b.kernelPath, b.uruncJSONPath, b.initrdPath)
	if err != nil {
		return fmt.Errorf("failed to extract boot files from rootfs: %w", err)
	}

	err = unmount(b.mountedPath)
	if err != nil {
		return fmt.Errorf("failed to unmount rootfs: %w", err)
	}

	return nil
}

func (b blockRootfs) postSetup() error {
	return nil
}

func (b blockRootfs) getMounts() ([]specs.Mount, error) {
	return []specs.Mount{tmpfsMount("/tmp", tmpfsSizeForBlockRootfs)}, nil
}

func (b blockRootfs) getBlockDevs() ([]types.BlockDevParams, error) {
	var blockArgs []types.BlockDevParams
	rootfsBlock := types.BlockDevParams{
		Source:     b.path,
		MountPoint: "/",
		ID:         "rootfs",
	}

	// NOTE: Rumprun does not allow us to mount
	// anything at '/'. As a result, we use the
	// /data mount point for Rumprun. For all the
	// other guests we use '/'.
	if b.guestType == "rumprun" {
		rootfsBlock.MountPoint = "/data"
	}

	blockArgs = append(blockArgs, rootfsBlock)
	blockFromMounts, err := getBlockVolumes(b.mounts, b.guest)
	if err != nil {
		return nil, err
	}
	blockArgs = append(blockArgs, blockFromMounts...)

	return blockArgs, nil
}

// TODO: Return an array instead of a single struct
func (b blockRootfs) getSharedDirs() (types.SharedfsParams, error) {
	return types.SharedfsParams{}, nil
}

func (b blockRootfs) preStartCmd() []string {
	return nil
}

// Taken from https://github.com/containerd/containerd/blob/v1.7.34/mount/mount_linux.go#L203
// and we simply change the timeout period to max 200 ms, then EBUSY is returned.
func unmount(target string) error {
	for i := 0; i < 10; i++ {
		// Always aim for strict unmount
		err := unix.Unmount(target, 0)
		if err != nil {
			switch err {
			case unix.EBUSY:
				time.Sleep(20 * time.Millisecond)
				continue
			default:
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("failed to unmount target %s: %w", target, unix.EBUSY)
}
