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
	"path/filepath"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// TODO: Find and set the correct size for the tmpfs in the host
const tmpfsSizeFor9pfsRootfs = "65536k"

var spawnProcessHook = spawnProcess

type sharedfsRootfs struct {
	mounts      []specs.Mount
	vfsdConfig  types.ExtraBinConfig
	sharedPath  string
	monRootfs   string
	mountedPath string
	sfsType     string
	memory      uint64
}

func (s sharedfsRootfs) preSetup() error {
	return nil
}

func (s sharedfsRootfs) postSetup() error {
	return nil
}

func (s sharedfsRootfs) getMounts() ([]specs.Mount, error) {
	// Mount the container's rootfs inside the monitor rootfs and then the
	// container's volumes on top of it.
	mounts := []specs.Mount{bindMount(s.mountedPath, containerRootfsMountPath, true)}

	if s.sfsType == "virtiofs" {
		// Get the virtiofsd binary from host in monRootfs
		mounts = append(mounts, bindMount(s.vfsdConfig.Path, s.vfsdConfig.Path, true))
	}

	tmpfsSize := chooseTmpfsSize(s.sfsType, s.memory)
	mounts = append(mounts, tmpfsMount("/tmp", tmpfsSize))
	mounts = append(mounts, filterBindMounts(s.mounts)...)

	return mounts, nil
}

func (s sharedfsRootfs) getBlockDevs() ([]types.BlockDevParams, error) {
	return nil, nil
}

func (s sharedfsRootfs) getSharedDirs() (types.SharedfsParams, error) {
	return types.SharedfsParams{
		Path: containerRootfsMountPath,
		Type: s.sfsType,
	}, nil
}

func (s sharedfsRootfs) preStart() error {
	if s.sfsType == "9pfs" {
		return nil
	}
	// Start the virtiofsd process
	args := []string{
		"--socket-path=/tmp/vhostqemu",
		"--shared-dir",
		s.sharedPath,
	}

	if s.vfsdConfig.Options != "" {
		args = append(args, strings.Fields(s.vfsdConfig.Options)...)
	}

	err := spawnProcessHook(s.vfsdConfig.Path, args)
	if err != nil {
		err = fmt.Errorf("failed to start virtiofsd: %w", err)
	}
	return err
}

func chooseTmpfsSize(sfsType string, mem uint64) string {
	if sfsType == "9pfs" {
		return tmpfsSizeFor9pfsRootfs
	}

	// For virtiofs, Qemu and virtiofsd are using a host file
	// to share the VM's RAM and hence the size of this file
	// should be the same as guest's memory. This file will
	// be placed under /tmp and we need to mount /tmp with enough
	// memory for this.
	// However, since /tmp might be used from the monitors for other
	// things too, we add one more MB extra.
	tmpMountMem := mem + (1024 * 1024)
	tmpMountMemStr := hypervisors.BytesToStringMB(tmpMountMem) + "m"

	return tmpMountMemStr
}

// adjustPathsForSharedFS updates paths to be relative to container rootfs mount
func adjustPathsForSharedfs(path string) string {
	if path != "" {
		return filepath.Join(containerRootfsMountPath, path)
	}

	return path
}

// filterBindMounts filters the mounts form the container's spec keeping only the
// bind mounts and adjusts the Destination path to the mountpoint of the
// container's rootfs inside the monitor rootfs.
func filterBindMounts(mounts []specs.Mount) []specs.Mount {
	var result []specs.Mount
	for _, m := range mounts {
		// Skip non-bind mounts
		// TODO handle other types of mounts too
		if m.Type != "bind" {
			continue
		}
		result = append(result, specs.Mount{
			Type:        "bind",
			Source:      m.Source,
			Destination: filepath.Join(containerRootfsMountPath, m.Destination),
			Options:     m.Options,
		})
	}

	return result
}
