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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/urunc-dev/urunc/internal/constants"
)

var StaticIPAddr = fmt.Sprintf("%s/24", constants.StaticNetworkTapIP)

type StaticNetwork struct {
}

// Apply the following rule:
// iptables -t nat -A POSTROUTING -o <IF> -s <IP> -j MASQUERADE --wait 1
// and write 1 to /proc/sys/net/ipv4/ip_forward to enable IP forwarding.
func setNATRule(iface string, sourceIP string) error {
	var args []string
	var stdout, stderr bytes.Buffer

	path, err := exec.LookPath("iptables")
	if err != nil {
		return err
	}

	file, err := os.OpenFile("/proc/sys/net/ipv4/ip_forward", os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open /proc/sys/net/ipv4/ip_forward: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString("1")
	if err != nil {
		return fmt.Errorf("failed to enable IP forwarding: %w", err)
	}
	netlog.Debug("Enabled IP forwarding")

	args = append(args, path)
	args = append(args, "-t")
	args = append(args, "nat")
	args = append(args, "-A")
	args = append(args, "POSTROUTING")
	args = append(args, "-s")
	args = append(args, sourceIP)
	args = append(args, "-o")
	args = append(args, iface)
	args = append(args, "-j")
	args = append(args, "MASQUERADE")
	args = append(args, "--wait")
	args = append(args, "1")

	cmd := exec.Cmd{
		Path:   path,
		Args:   args,
		Stdout: &stdout,
		Stderr: &stderr,
	}
	err = cmd.Run()
	if err != nil {
		switch err.(type) {
		case *exec.ExitError:
			return fmt.Errorf("iptables command %s failed: %s", cmd.String(), stderr.String())
		default:
			return err
		}
	}

	netlog.Debug("Applied iptables rule for NAT")

	return nil
}

func (n StaticNetwork) NetworkSetup(uid uint32, gid uint32) (*UnikernelNetworkInfo, error) {
	newTapName := strings.ReplaceAll(DefaultTap, "X", "0")
	addTCRules := false
	redirectLink, err := discoverContainerIface()
	if err != nil {
		netlog.Errorf("failed to find container interface, (unikernel may have been spawned using ctr): %v", err)
		return nil, err
	}
	newTapDevice, err := networkSetup(newTapName, StaticIPAddr, redirectLink, addTCRules, uid, gid)
	if err != nil {
		return nil, err
	}
	err = setNATRule(redirectLink.Attrs().Name, StaticIPAddr)
	if err != nil {
		return nil, err
	}
	return &UnikernelNetworkInfo{
		TapDevice: newTapDevice.Attrs().Name,
		EthDevice: Interface{
			IP:             constants.StaticNetworkUnikernelIP,
			DefaultGateway: constants.StaticNetworkTapIP,
			Mask:           "255.255.255.0",
			Interface:      redirectLink.Attrs().Name, // or tap0_urunc?
			MAC:            redirectLink.Attrs().HardwareAddr.String(),
		},
	}, nil
}
