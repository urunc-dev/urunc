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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/urunc-dev/urunc/internal/constants"
)

var StaticIPAddr = fmt.Sprintf("%s/24", constants.StaticNetworkTapIP)

type StaticNetwork struct {
}

// natRuleArgs returns the common iptables arguments identifying the NAT
// rule that setNATRule applies, minus the leading action flag (e.g. "-A"
// or "-C"), so that the same filter can be used both to check for the
// rule's presence and to append it.
func natRuleArgs(iface string, sourceIP string) []string {
	return []string{
		"-t", "nat",
		"POSTROUTING",
		"-s", sourceIP,
		"-o", iface,
		"-j", "MASQUERADE",
		"--wait", "1",
	}
}

// natRuleExists checks whether the NAT rule identified by ruleArgs is
// already present in the POSTROUTING chain, via "iptables -C". iptables
// exits with 0 if the rule exists and with 1 if it does not.
func natRuleExists(path string, ruleArgs []string) (bool, error) {
	var stdout, stderr bytes.Buffer

	args := append([]string{path, "-C"}, ruleArgs...)
	cmd := exec.Cmd{
		Path:   path,
		Args:   args,
		Stdout: &stdout,
		Stderr: &stderr,
	}
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("iptables command %s failed: %s", cmd.String(), stderr.String())
}

// Apply the following rule, if not already present:
// iptables -t nat -A POSTROUTING -o <IF> -s <IP> -j MASQUERADE --wait 1
// and write 1 to /proc/sys/net/ipv4/ip_forward to enable IP forwarding.
//
// Since the network namespace, and therefore any iptables rules in it,
// persists across container restarts in Kubernetes (see the equivalent
// TAP device leak fixed for #406), we check whether the rule already
// exists before appending it, to avoid piling up duplicate rules on
// every restart.
func setNATRule(iface string, sourceIP string) error {
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

	ruleArgs := natRuleArgs(iface, sourceIP)

	exists, err := natRuleExists(path, ruleArgs)
	if err != nil {
		return err
	}
	if exists {
		netlog.Debug("iptables NAT rule already present, skipping")
		return nil
	}

	args := append([]string{path, "-A"}, ruleArgs...)
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
