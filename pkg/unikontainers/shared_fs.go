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
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func chooseTmpfsSize(mem uint64) string {
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

func setupSharedfsBasedRootfs(rfs types.RootfsParams, vfsdBin string, mounts []specs.Mount) error {
	// Mount the container's image rootfs inside the monitor rootfs
	err := fileFromHost(rfs.MonRootfs, rfs.MountedPath, containerRootfsMountPath, unix.MS_BIND|unix.MS_PRIVATE, false)
	if err != nil {
		return err
	}

	newCntrRootfs := filepath.Join(rfs.MonRootfs, containerRootfsMountPath)
	err = mountVolumes(newCntrRootfs, mounts)
	if err != nil {
		return err
	}

	if rfs.Type == "virtiofs" {
		// Get the virtiofsd binary from host in monRootfs
		err = fileFromHost(rfs.MonRootfs, vfsdBin, "", unix.MS_BIND|unix.MS_PRIVATE, false)
		if err != nil {
			return fmt.Errorf("Could not bind mount %s: %w", vfsdBin, err)
		}
	}

	return nil
}

// adjustPathsForSharedFS updates paths to be relative to container rootfs mount
func adjustPathsForSharedfs(path string) string {
	if path != "" {
		return filepath.Join(containerRootfsMountPath, path)
	}

	return path
}
