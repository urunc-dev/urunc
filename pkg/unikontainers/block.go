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

	"github.com/moby/sys/mount"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

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
		if len(preDash) < 5 {
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
			}).Debug("Found block device")

			blockDev.Source = postDash[1]
			blockDev.FsType = postDash[0]
			blockDev.MountPoint = path
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

	err = mount.Unmount(b.mountedPath)
	if err != nil {
		return fmt.Errorf("failed to unmount rootfs: %w", err)
	}

	return nil
}

func (b blockRootfs) postSetup() error {
	if b.mountedPath != "" {
		err := setupDev(b.monRootfs, b.path)
		if err != nil {
			return err
		}
	}

	return nil
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
	blockFromMounts, err := getBlockVolumes(b.monRootfs, b.mounts, b.guest)
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

func (b blockRootfs) preStart() error {
	return nil
}
