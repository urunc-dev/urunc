//go:build linux
// +build linux

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
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/moby/sys/mountinfo"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/urunc-dev/urunc/pkg/unikontainers/initrd"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

// TODO: Find and set the correct size for the tmpfs in the host
const tmpfsSizeForBlockRootfs = "65536k"

const (
	containerBootInitrdPath = "/boot/urunc-container-initrd"
	containerCmdPath        = "/urunc-cmd"
	containerEnvPath        = "/urunc-env"
	containerResolvPath     = "/urunc-resolv.conf"
	containerHostsPath      = "/urunc-hosts"
	containerHostnamePath   = "/urunc-hostname"
)

var ErrMountpoint = errors.New("no FS is mounted in this mountpoint")

type blockRootfs struct {
	mounts          []specs.Mount
	monRootfs       string
	mountedPath     string
	containerRootfs string
	path            string
	kernelPath      string
	initrdPath      string
	uruncJSONPath   string
	guestType       string
	guest           types.Unikernel
	bootKernelHost  string
	bootInitrdHost  string
	containerCmd    []string
	containerEnv    []string
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
	// Resolve every boot file both in the source rootfs and in the new rootfs
	// with SecureJoin.
	currentUnikernelPath, err := securejoin.SecureJoin(rootfsPath, unikernel)
	if err != nil {
		return err
	}
	targetUnikernelPath, err := securejoin.SecureJoin(newRootfsPath, unikernel)
	if err != nil {
		return err
	}
	err = moveFile(currentUnikernelPath, targetUnikernelPath)
	if err != nil {
		return fmt.Errorf("could not move %s to %s: %w", currentUnikernelPath, targetUnikernelPath, err)
	}

	if initrd != "" {
		currentInitrdPath, err := securejoin.SecureJoin(rootfsPath, initrd)
		if err != nil {
			return err
		}
		targetInitrdPath, err := securejoin.SecureJoin(newRootfsPath, initrd)
		if err != nil {
			return err
		}
		err = moveFile(currentInitrdPath, targetInitrdPath)
		if err != nil {
			return fmt.Errorf("could not move %s to %s: %w", currentInitrdPath, targetInitrdPath, err)
		}
	}

	currentConfigPath, err := securejoin.SecureJoin(rootfsPath, uruncJSON)
	if err != nil {
		return err
	}
	targetConfigPath, err := securejoin.SecureJoin(newRootfsPath, uruncJSON)
	if err != nil {
		return err
	}
	err = moveFile(currentConfigPath, targetConfigPath)
	if err != nil {
		return fmt.Errorf("could not move %s to %s: %w", currentConfigPath, targetConfigPath, err)
	}

	return nil
}

func (b blockRootfs) hasContainerBoot() bool {
	return b.bootKernelHost != "" || b.bootInitrdHost != ""
}

// bootStageDir is where the host boot files are staged so that, after the
// pivot, Exec resolves them under containerRootfsMountPath: the extraction
// directory of a snapshot rootfs, or the container rootfs itself when that is
// what gets bind-mounted there (explicit block image).
func (b blockRootfs) bootStageDir() string {
	if b.mountedPath == "" && b.containerRootfs != "" {
		return b.containerRootfs
	}
	return filepath.Join(b.monRootfs, containerRootfsMountPath)
}

func (b blockRootfs) stageContainerBootFiles() error {
	if b.bootKernelHost == "" || b.bootInitrdHost == "" {
		return fmt.Errorf("%s and %s must be set together", annotBootKernel, annotBootInitrd)
	}
	if b.kernelPath == "" {
		return fmt.Errorf("container boot requires a destination path via %s", annotBinary)
	}

	stageDir := b.bootStageDir()
	if err := copyFile(b.bootKernelHost, filepath.Join(stageDir, b.kernelPath)); err != nil {
		return fmt.Errorf("could not stage host boot kernel %s: %w", b.bootKernelHost, err)
	}
	initrdDst := filepath.Join(stageDir, containerBootInitrdPath)
	if err := copyFile(b.bootInitrdHost, initrdDst); err != nil {
		return fmt.Errorf("could not stage host boot initrd %s: %w", b.bootInitrdHost, err)
	}
	if len(b.containerCmd) > 0 {
		if err := initrd.AddFileToInitrd(initrdDst, strings.Join(b.containerCmd, "\n")+"\n", containerCmdPath); err != nil {
			return fmt.Errorf("could not add container command to boot initrd: %w", err)
		}
	}
	if len(b.containerEnv) > 0 {
		if err := initrd.AddFileToInitrd(initrdDst, strings.Join(b.containerEnv, "\n")+"\n", containerEnvPath); err != nil {
			return fmt.Errorf("could not add container environment to boot initrd: %w", err)
		}
	}
	return b.stageContainerNetworkFiles(initrdDst)
}

var containerNetworkFiles = map[string]string{
	"/etc/resolv.conf": containerResolvPath,
	"/etc/hosts":       containerHostsPath,
	"/etc/hostname":    containerHostnamePath,
}

func (b blockRootfs) stageContainerNetworkFiles(initrdPath string) error {
	stagedResolver := false
	for _, m := range b.mounts {
		dest, ok := containerNetworkFiles[m.Destination]
		if !ok || m.Source == "" {
			continue
		}
		data, err := os.ReadFile(m.Source)
		if err != nil || len(data) == 0 {
			continue
		}
		if err := initrd.AddFileToInitrd(initrdPath, string(data), dest); err != nil {
			return fmt.Errorf("could not add %s to boot initrd: %w", m.Destination, err)
		}
		stagedResolver = stagedResolver || m.Destination == "/etc/resolv.conf"
	}
	if !stagedResolver {
		if resolv := usableHostResolvConf(); resolv != "" {
			return initrd.AddFileToInitrd(initrdPath, resolv, containerResolvPath)
		}
	}
	return nil
}

func usableHostResolvConf() string {
	for _, path := range []string{"/run/systemd/resolve/resolv.conf", "/etc/resolv.conf"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var lines []string
		hasNameserver := false
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			switch fields[0] {
			case "nameserver":
				ip := net.ParseIP(fields[1])
				if ip == nil || ip.IsLoopback() {
					continue
				}
				hasNameserver = true
				lines = append(lines, line)
			case "search", "options":
				lines = append(lines, line)
			}
		}
		if hasNameserver {
			return strings.Join(lines, "\n") + "\n"
		}
	}
	return ""
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
		IsExplicit: true,
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
			mInfo.IsExplicit = false
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
	if b.mountedPath == "" && !b.hasContainerBoot() {
		return nil
	}

	if b.mountedPath != "" {
		if err := copyMountfiles(b.mountedPath, b.mounts); err != nil {
			return fmt.Errorf("failed to copy files from mount list: %w", err)
		}
	}

	// Extract the boot files under containerRootfsMountPath
	// FIXME: This approach fills up /run with unikernel binaries and
	// urunc.json files for each unikernel instance we run
	if b.hasContainerBoot() {
		if err := b.stageContainerBootFiles(); err != nil {
			return err
		}
	} else if err := extractBootFiles(b.mountedPath, filepath.Join(b.monRootfs, containerRootfsMountPath), b.kernelPath, b.uruncJSONPath, b.initrdPath); err != nil {
		return fmt.Errorf("failed to extract boot files from rootfs: %w", err)
	}

	if b.mountedPath != "" {
		if err := unmount(b.mountedPath); err != nil {
			return fmt.Errorf("failed to unmount rootfs: %w", err)
		}
	}

	return nil
}

func (b blockRootfs) postSetup() error {
	return nil
}

func (b blockRootfs) getMounts() ([]specs.Mount, error) {
	mounts := []specs.Mount{tmpfsMount("/tmp", tmpfsSizeForBlockRootfs)}

	if b.mountedPath == "" {
		// In the case of explicit block image the kernel and the block
		// image are in the container's rootfs, so bind-mount container's
		// rootfs into the monitor rootfs
		mounts = append(mounts, bindMount(b.containerRootfs, containerRootfsMountPath, true, false, "nodev", "nosuid", "noexec"))
	}

	return mounts, nil
}

func (b blockRootfs) getBlockDevs() ([]types.BlockDevParams, error) {
	var blockArgs []types.BlockDevParams
	rootfsBlock := types.BlockDevParams{
		Source:     b.path,
		MountPoint: "/",
		ID:         "rootfs",
		// An empty mountedPath means that the block image was not mounted
		// and therefore it is an explicit image inside the container's rootfs
		IsExplicit: b.mountedPath == "",
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
