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
	"hash/fnv"
	"math"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

// ErrVAccelDisabled is returned by resolveVAccelConfig when the vAccel
// annotation is absent. This is an expected condition, not a misconfiguration.
var ErrVAccelDisabled = errors.New("vaccel is disabled")

// The vAccel RPC address ends up in the guest's cmdline and, in the case of
// firecracker, names the host unix socket of the vAccel agent, which gets bind
// mounted in the monitor's rootfs. Therefore, we accept only the very specific
// patterns below.
const (
	// vAccelPortRe matches a port number (1-65535), without leading zeros.
	vAccelPortRe = `(?:[1-9]\d{0,3}|[1-5]\d{4}|6[0-4]\d{3}|65[0-4]\d{2}|655[0-2]\d|6553[0-5])`

	// vAccelSockDirRe matches the host directory of the vAccel unix socket. We
	// allow only plain absolute paths, without dots, whitespace or shell
	// metacharacters.
	vAccelSockDirRe = `(?:/[A-Za-z0-9_-]+)+`

	// vAccelUnixScheme is the scheme of the RPC addresses that name a unix socket.
	vAccelUnixScheme = "unix://"
)

// vAccelAddressRe maps a monitor to the format that it expects the vAccel RPC
// address to have. A monitor which is not in the map does not support vAccel.
var vAccelAddressRe = map[string]*regexp.Regexp{
	"qemu":        regexp.MustCompile(`^vsock://2:` + vAccelPortRe + `$`),
	"firecracker": regexp.MustCompile(`^` + vAccelUnixScheme + `(` + vAccelSockDirRe + `)/vaccel\.sock_(` + vAccelPortRe + `)$`),
}

// Guest CIDs (vsock Context Identifiers) are 32-bit. 0 and 1 are reserved and 2
// is the host, so a guest may use anything from 3 up to 2^32-2 (2^32-1 means
// "any"). The CID lives in the host's global vsock namespace, so two running
// VMs must not share one.
const (
	minGuestCID = 3
	maxGuestCID = math.MaxUint32 - 1
)

// idToGuestCID derives a deterministic guest CID for vsock communication from
// a container or VM ID, so that every consumer (the monitor, urunc exec,
// vAccel) computes the same value without sharing state. The ID is hashed with
// FNV-1a and mapped onto the whole valid CID range: with 2^32-4 possible
// values the chance that two concurrently running containers collide is
// negligible, whereas a small range (this used to be 3..99, with a character
// sum) collided for a handful of containers.
func idToGuestCID(id string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))

	const valRange = uint64(maxGuestCID - minGuestCID + 1)
	val := uint64(h.Sum32())%valRange + minGuestCID

	return int(val) //nolint:gosec // val fits in 32 bits by construction
}

// isValidVSockAddress validates a vsock address string and ensures
// it matches the expected format for the selected hypervisor.
// For firecracker, it also replaces the RPC address with the
// corresponding vsock address, and returns the host path of the agent's
// unix socket, which must later be bind-mounted into the monitor rootfs.
func isValidVSockAddress(rpcAddress *string, hypervisor string) (bool, string, error) {
	regex, exists := vAccelAddressRe[hypervisor]
	if !exists {
		return false, "", fmt.Errorf("unsupported hypervisor: %q", hypervisor)
	}

	if !regex.MatchString(*rpcAddress) {
		return false, "", fmt.Errorf("rpc address %q does not match the expected format for %s", *rpcAddress, hypervisor)
	}

	if hypervisor == "firecracker" {
		// The address matched, hence the groups of the regex are present.
		matches := regex.FindStringSubmatch(*rpcAddress)
		hostSocket := strings.TrimPrefix(*rpcAddress, vAccelUnixScheme)
		*rpcAddress = "vsock://2:" + matches[2]

		return true, hostSocket, nil
	}

	return true, "", nil
}

// resolveVAccelConfig parses and validates vAccel-related annotations,
// resolves the RPC address based on the selected hypervisor,
// and returns the vAccel type (e.g., "vsock"), the host path of the agent's
// unix socket to be bind-mounted (firecracker only) and the normalized RPC
// address to be exported to the guest.
func resolveVAccelConfig(hypervisor string, annotations map[string]string) (string, string, string, error) {
	var err error
	var success bool
	var vsockSocketPath string

	address := annotations[annotRPCAddress]

	vAccelType, exists := annotations[annotVAccel]
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

// checkVAccelSocket makes sure that path is a unix socket, i.e. the vAccel
// agent is listening there. Anything else is refused.
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
