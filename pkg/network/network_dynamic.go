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

package network

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/urunc-dev/urunc/internal/constants"
	"github.com/vishvananda/netlink"
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
		return nil, err
	}
	if tapIndex > 0 {
		return nil, fmt.Errorf("unsupported operation: can't spawn multiple unikernels in the same network namespace")
	}
	redirectLink, err := netlink.LinkByName(DefaultInterface)
	if err != nil {
		netlog.Errorf("failed to find %s interface", DefaultInterface)
		return nil, err
	}
	newTapName := strings.ReplaceAll(DefaultTap, "X", strconv.Itoa(tapIndex))
	ipTemplate := fmt.Sprintf("%s/24", constants.DynamicNetworkTapIP)
	newIPAddr := strings.ReplaceAll(ipTemplate, "X", strconv.Itoa(tapIndex+1))
	newTapDevice, err := networkSetup(newTapName, newIPAddr, redirectLink, true, uid, gid)
	if err != nil {
		return nil, err
	}
	ifInfo, err := getInterfaceInfo(DefaultInterface)
	if err != nil {
		return nil, err
	}
	return &UnikernelNetworkInfo{
		TapDevice: newTapDevice.Attrs().Name,
		EthDevice: ifInfo,
	}, nil
}
