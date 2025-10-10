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

// getBlockDevice checks if a path is a block-based mount point
// If the path is indeed a mount point then it returns the information of this
// block device in the form of types.BlockDevParams
// If path is not a mount point of a block device or in case of error,
// it returns an empty types.BlockDevParams struct and an error.
func getBlockDevice(path string) (types.BlockDevParams, error) {
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
			Image:      fields[1],
			FsType:     fields[0],
			MountPoint: "/",
			ID:         0,
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

func handleExplicitBlockImage(blockImg string, mountPoint string) types.BlockDevParams {
	blockInstance := types.BlockDevParams{}
	if blockImg == "" {
		return blockInstance
	}
	blockInstance.Image = blockImg
	if mountPoint != "" {
		blockInstance.MountPoint = mountPoint
	} else {
		// NOTE: If the user has not specified a mountpoint for
		// the block image, set it to /data mostly for keeping
		// compatibility with old images. However, we might need to
		// revisit this and return an error instead.
		blockInstance.MountPoint = "/data"
	}
	if blockInstance.MountPoint == "/" {
		blockInstance.ID = 0
	} else {
		blockInstance.ID = 1
	}

	return blockInstance
}
