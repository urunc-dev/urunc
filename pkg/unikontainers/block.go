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
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/sys/mount"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

var ErrMountpoint = errors.New("no FS is mounted in this mountpoint")

// getMountInfo checks if the path (given as argument) is a mountpoint
// looking at /proc/self/mountinfo.
// If the path is indeed a mount point then getMountInfo stores and returns
// the respective info in a BlockDevParams struct.
// If the path is not a mount point (not present in /proc/self/mountinfo)
// then getMountInfo returns an empty BlockDevParams struct and ErrMountpoint error.
func getMountInfo(path string) (types.BlockDevParams, error) {
	selfProcMountInfo := "/proc/self/mountinfo"

	file, err := os.Open(selfProcMountInfo)
	if err != nil {
		return types.BlockDevParams{}, fmt.Errorf("failed to open mountinfo: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, " - ")
		if len(parts) != 2 {
			return types.BlockDevParams{}, fmt.Errorf("invalid mountinfo line in /proc/self/mountinfo")
		}

		fields := strings.Fields(parts[0])
		if len(fields) < 5 || fields[4] != path {
			continue
		}
		fields = strings.Fields(parts[1])
		if len(fields) < 2 {
			continue
		}
		uniklog.WithFields(logrus.Fields{
			"mounted at": path,
			"device":     fields[1],
			"fstype":     fields[0],
		}).Debug("Found container rootfs mount")

		return types.BlockDevParams{
			Source:     fields[1],
			FsType:     fields[0],
			MountPoint: path,
			ID:         "",
		}, nil
	}

	return types.BlockDevParams{}, ErrMountpoint
}

// extractUnikernelFromBlock moves unikernel binary, initrd and urunc.json
// files from old rootfsPath to newRootfsPath
// FIXME: This approach fills up /run with unikernel binaries, initrds and urunc.json
// files for each unikernel we run
func extractFilesFromBlock(rootfsPath string, newRootfsPath string, unikernel string, uruncJSON string, initrd string) error {
	currentUnikernelPath := filepath.Join(rootfsPath, unikernel)
	targetUnikernelPath := filepath.Join(newRootfsPath, unikernel)
	targetUnikernelDir, _ := filepath.Split(targetUnikernelPath)
	err := moveFile(currentUnikernelPath, targetUnikernelDir)
	if err != nil {
		return fmt.Errorf("Could not move %s to %s: %w", currentUnikernelPath, targetUnikernelPath, err)
	}

	if initrd != "" {
		currentInitrdPath := filepath.Join(rootfsPath, initrd)
		targetInitrdPath := filepath.Join(newRootfsPath, initrd)
		targetInitrdDir, _ := filepath.Split(targetInitrdPath)
		err = moveFile(currentInitrdPath, targetInitrdDir)
		if err != nil {
			return fmt.Errorf("Could not move %s to %s: %w", currentInitrdPath, targetInitrdPath, err)
		}
	}

	currentConfigPath := filepath.Join(rootfsPath, uruncJSON)
	err = moveFile(currentConfigPath, newRootfsPath)
	if err != nil {
		return fmt.Errorf("Could not move %s to %s: %w", currentConfigPath, newRootfsPath, err)
	}

	return nil
}

// prepareDMAsBLock copies the files needed for the unikernel boot (e.g.
// unikernel binary, initrd file) and the urunc.json file in a new temporary
// directory. Then it unmounts the devmapper device and renames the temporary
// directory as the container rootfs. This is needed to keep the same paths
// for the unikernel files.
func prepareDMAsBlock(rootfsPath string, newRootfsPath string, unikernel string, uruncJSON string, initrd string) error {
	// extract unikernel
	// FIXME: This approach fills up /run with unikernel binaries and
	// urunc.json files for each unikernel instance we run
	err := extractFilesFromBlock(rootfsPath, newRootfsPath, unikernel, uruncJSON, initrd)
	if err != nil {
		return err
	}
	// unmount block device
	// FIXME: umount and rm might need some retries
	err = mount.Unmount(rootfsPath)
	if err != nil {
		return err
	}

	return nil
}

func copyMountfiles(targetPath string, mounts []specs.Mount) error {
	for _, m := range mounts {
		if m.Type != "bind" {
			continue
		}
		err := fileFromHost(targetPath, m.Source, m.Destination, 0, true)
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

func handleCntrRootfsAsBlock(rfs types.RootfsParams, unikernelType string, unikernelPath string, uruncJSONFilename string, initrdPath string, mounts []specs.Mount) (types.BlockDevParams, error) {
	err := copyMountfiles(rfs.MountedPath, mounts)
	if err != nil {
		return types.BlockDevParams{}, err
	}

	err = prepareDMAsBlock(rfs.MountedPath, rfs.MonRootfs, unikernelPath, uruncJSONFilename, initrdPath)
	if err != nil {
		return types.BlockDevParams{}, err
	}

	err = setupDev(rfs.MonRootfs, rfs.Path)
	if err != nil {
		return types.BlockDevParams{}, err
	}

	mp := "/"
	// NOTE: Rumprun does not allow us to mount
	// anything at '/'. As a result, we use the
	// /data mount point for Rumprun. For all the
	// other guests we use '/'.
	if unikernelType == "rumprun" {
		mp = "/data"
	}

	return types.BlockDevParams{
		Source:     rfs.Path,
		MountPoint: mp,
		ID:         "rootfs",
	}, nil
}

// Search all the mount entries in the container's config and
// find the ones that come from a block.
func getBlockVolumes(monRootfs string, mounts []specs.Mount, ukernel types.Unikernel) ([]types.BlockDevParams, error) {
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
			err = mount.Unmount(mInfo.MountPoint)
			if err != nil {
				return nil, err
			}
			err = setupDev(monRootfs, mInfo.Source)
			if err != nil {
				return nil, err
			}
			mInfo.ID = fmt.Sprintf("vol%d", i)
			mInfo.MountPoint = m.Destination
			blkImgs = append(blkImgs, mInfo)
		}
	}

	return blkImgs, nil
}

func handleBlockBasedRootfs(rfs types.RootfsParams, ukernel types.Unikernel, unikernelType string, unikernelPath string, uruncJSONFilename string, initrdPath string, mounts []specs.Mount) ([]types.BlockDevParams, error) {
	var blockArgs []types.BlockDevParams
	var rootfsBlock types.BlockDevParams
	var err error
	if rfs.MountedPath == "" {
		// The Mountpoint in the annotation was "/" and hence the rootfs
		// of the guest is a block Image inside the container's image.
		rootfsBlock, err = handleExplicitBlockImage(rfs.Path, "/")
	} else {
		rootfsBlock, err = handleCntrRootfsAsBlock(rfs, unikernelType, unikernelPath, uruncJSONFilename, initrdPath, mounts)
	}
	if err != nil {
		return nil, err
	}
	rootfsBlock.ID = "rootfs"
	blockArgs = append(blockArgs, rootfsBlock)
	blockFromMounts, err := getBlockVolumes(rfs.MonRootfs, mounts, ukernel)
	if err != nil {
		return nil, err
	}
	blockArgs = append(blockArgs, blockFromMounts...)

	return blockArgs, nil
}
