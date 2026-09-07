//go:build linux

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

// Linux realization of the vAccel vsock setup: the host device nodes and the
// agent socket bind mount inside the monitor rootfs.

package unikontainers

import (
	"fmt"
	"path/filepath"

	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

func checkVAccelSocket(path string) error {
	var st unix.Stat_t
	err := unix.Stat(path, &st)
	if err != nil {
		return fmt.Errorf("could not find the vAccel socket %s: %w", path, err)
	}

	if st.Mode&unix.S_IFMT != unix.S_IFSOCK {
		return fmt.Errorf("%s is not a unix socket", path)
	}

	return nil
}

// prepareVSockEnvironment prepares all required vsock devices and mounts
// for vAccel execution inside the guest. This includes /dev/vsock,
// /dev/vhost-vsock, and (for firecracker) making the agent's unix socket
// reachable from the monitor.
func prepareVSockEnvironment(monRootfs string, hypervisor string, hostSocket string) ([]specs.LinuxDevice, error) {
	vsockDev, err := deviceFromHost("/dev/vsock")
	if err != nil {
		return nil, fmt.Errorf("could not get host device /dev/vsock: %w", err)
	}
	vhostSockDev, err := deviceFromHost("/dev/vhost-vsock")
	if err != nil {
		return nil, fmt.Errorf("could not get host device /dev/vhost-vsock: %w", err)
	}
	devices := []specs.LinuxDevice{vsockDev, vhostSockDev}

	// Bind mount the agent's unix socket RO (connecting to it does not need a
	// writable mount) at a fixed path in the monitor rootfs, where firecracker
	// also creates its own listening socket. applyMounts creates the parent
	// directory (vAccelMountPath) and the socket mountpoint under monRootfs.
	if hypervisor == "firecracker" {
		err = checkVAccelSocket(hostSocket)
		if err != nil {
			return nil, err
		}

		sockMountPoint := filepath.Join(vAccelMountPath, filepath.Base(hostSocket))
		mounts := []specs.Mount{
			bindMount(hostSocket, sockMountPoint, true, true),
		}
		err = applyMounts(monRootfs, mounts)
		if err != nil {
			return nil, err
		}
	}

	return devices, nil
}
