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
	"errors"
	"fmt"
	"regexp"

	"golang.org/x/sys/unix"
)

// ErrVAccelDisabled is returned by resolveVAccelConfig when the vAccel
// annotation is absent. This is an expected condition, not a misconfiguration.
var ErrVAccelDisabled = errors.New("vaccel is disabled")

// idToGuestCID generates a deterministic guest CID (Context Identifier)
// for vsock communication based on a container or VM ID.
func idToGuestCID(id string) int {
	sum := 0
	for _, c := range id {
		sum += int(c)
	}
	const minVal = 3
	const maxVal = 99
	const valRange = maxVal - minVal + 1
	val := (sum % valRange) + minVal

	return val
}

// isValidVSockAddress validates a vsock address string and ensures
// it matches the expected format for the selected hypervisor.
// For firecracker, it also replaces the RPC address with the
// corresponding vsock address, and returns the directory path of the
// unix socket, which must later be bind-mounted into the guest rootfs.
func isValidVSockAddress(rpcAddress *string, hypervisor string) (bool, string, error) {
	var regex *regexp.Regexp

	switch hypervisor {
	case "qemu":
		regex = regexp.MustCompile(`^vsock://2:\d+$`)
	case "firecracker":
		regex = regexp.MustCompile(`^unix://(.*)/vaccel\.sock_(\d+)$`)
	default:
		return false, "", fmt.Errorf("unsupported hypervisor: %q", hypervisor)
	}

	if regex.MatchString(*rpcAddress) {
		if hypervisor == "firecracker" {
			matches := regex.FindStringSubmatch(*rpcAddress)
			if matches == nil {
				return false, "", fmt.Errorf("failed to parse rpc address %q for %s", *rpcAddress, hypervisor)
			}

			*rpcAddress = "vsock://2:" + matches[2]
			return true, matches[1], nil
		}
		return true, "", nil
	}
	return false, "", fmt.Errorf("rpc address %q does not match the expected format for %s", *rpcAddress, hypervisor)
}

// resolveVAccelConfig parses and validates vAccel-related annotations,
// resolves the RPC address based on the selected hypervisor,
// and returns the vAccel type (e.g., "vsock"), the unix socket path to be
// bind-mounted (Firecracker only) and the normalized RPC address to be
// exported to the guest.
func resolveVAccelConfig(hypervisor string, annotations map[string]string) (string, string, string, error) {
	var err error
	var success bool
	var vsockSocketPath string

	address := annotations["com.urunc.unikernel.RPCAddress"]

	vAccelType, exists := annotations["com.urunc.unikernel.vAccel"]
	if exists {
		if address == "" {
			err = fmt.Errorf("vaccel is enabled, but rpc address is not set")
			return vAccelType, "", "", err
		}
	} else {
		return "", "", "", ErrVAccelDisabled
	}

	if vAccelType == "vsock" {
		// validate address
		success, vsockSocketPath, err = isValidVSockAddress(&address, hypervisor)
		if !success {
			return vAccelType, "", "", err
		}
	}

	return vAccelType, vsockSocketPath, address, err
}

// prepareVSockEnvironment prepares all required vsock devices and mounts
// for vAccel execution inside the guest. This includes /dev/vsock,
// /dev/vhost-vsock, and (for Firecracker) binding the host unix socket.
func prepareVSockEnvironment(monRootfs string, hypervisor string, vsockSocketPath string) error {
	err := setupDev(monRootfs, "/dev/vsock")
	if err != nil {
		return err
	}

	err = setupDev(monRootfs, "/dev/vhost-vsock")
	if err != nil {
		return err
	}

	// bind mount the unix socket directory
	if hypervisor == "firecracker" {
		err = fileFromHost(monRootfs, vsockSocketPath, "", unix.MS_BIND|unix.MS_PRIVATE, false)
		if err != nil {
			return err
		}
	}
	return nil
}
