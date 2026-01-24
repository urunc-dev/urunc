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

package network

import (
	"fmt"
	"strconv"
	"strings"
)

type DynamicNetwork struct {
}

// NetworkSetup checks if any tap device is available in the current netns. If it is, it assumes a running unikernel
// is present in the current netns and returns an error, because network functionality for more than one unikernels
// is not yet implemented.
// If no TAP devices are available in the current netns, it creates a new tap device and
// sets TC rules between the veth interface and the tap device inside the namespace.
//
// FIXME: CUrrently only one tap device per netns can provide functional networking. We need to find a proper way to handle networking
// for multiple unikernels in the same pod/network namespace.
// See: https://github.com/urunc-dev/urunc/issues/13
func (n DynamicNetwork) NetworkSetup(uid uint32, gid uint32) (*UnikernelNetworkInfo, error) {
	tapIndex, err := getTapIndex()
	if err != nil {
		return nil, fmt.Errorf("getTapIndex failed: %w", err)
	}
	if tapIndex > 0 {
		return nil, fmt.Errorf("unsupported operation: can't spawn multiple unikernels in the same network namespace")
	}

	redirectLink, err := discoverContainerIface()
	if err != nil {
		return nil, fmt.Errorf("failed to find container interface, (unikernel may have been spawned using ctr): %w", err)
	}
	netlog.Debugf("found interface %s (index=%d)", redirectLink.Attrs().Name, redirectLink.Attrs().Index)

	newTapName := strings.ReplaceAll(DefaultTap, "X", strconv.Itoa(tapIndex))
	netlog.Debugf("creating tap device %s", newTapName)

	newTapDevice, err := networkSetup(newTapName, "", redirectLink, true, uid, gid)
	if err != nil {
		return nil, fmt.Errorf("networkSetup(%s) failed: %w", newTapName, err)
	}
	netlog.Debugf("tap device created: %s", newTapDevice.Attrs().Name)

	netlog.Debugf("fetching info for %s", redirectLink.Attrs().Name)
	ifInfo, err := getInterfaceInfo(redirectLink.Attrs().Name)
	if err != nil {
		return nil, fmt.Errorf("getInterfaceInfo(%s) failed: %w", redirectLink.Attrs().Name, err)
	}

	return &UnikernelNetworkInfo{
		TapDevice: newTapDevice.Attrs().Name,
		EthDevice: ifInfo,
	}, nil
}
