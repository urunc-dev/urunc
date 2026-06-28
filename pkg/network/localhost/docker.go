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

package localhost

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"

	"github.com/vishvananda/netlink"
)

func dockerRules(f *Forwarder) error {

	tcpPort, udpPort, err := dockerDNSPorts(f.LoIP)
	if err != nil {
		return err
	}
	lhlog.Debugf("Docker DNS resolver: tcp/%s udp/%s", tcpPort, udpPort)

	if err := dnat(f.VirtIP, "udp", net.JoinHostPort(f.LoIP.String(), udpPort)); err != nil {
		return err
	}
	if err := dnat(f.VirtIP, "tcp", net.JoinHostPort(f.LoIP.String(), tcpPort)); err != nil {
		return err
	}
	lhlog.Debug("Applied Docker DNAT rules")

	return nil
}

func dockerDNSPorts(loIP net.IP) (string, string, error) {
	var tcpPort, udpPort string

	// syscall.AF_INET : IPv4
	tcp, err := netlink.SocketDiagTCPInfo(syscall.AF_INET)
	if err != nil {
		return "", "", fmt.Errorf("could not list TCP sockets in namespace: %w", err)
	}
	for _, s := range tcp {
		if s.InetDiagMsg.ID.Source.String() == loIP.String() && s.InetDiagMsg.ID.Destination.String() == "0.0.0.0" && int(s.InetDiagMsg.State) == netlink.TCP_LISTEN {
			tcpPort = fmt.Sprintf("%d", s.InetDiagMsg.ID.SourcePort)
			break
		}
	}
	udp, err := netlink.SocketDiagUDPInfo(syscall.AF_INET)
	if err != nil {
		return "", "", fmt.Errorf("could not list UDP sockets in namespace: %w", err)
	}
	for _, s := range udp {

		// As udp has no connection states as tcp. by default (which is our case) it should have tcp_close (7) state to read it as "no peer association"
		// TODO: is there any other states that could make this condition fails ?
		if s.InetDiagMsg.ID.Source.String() == loIP.String() && s.InetDiagMsg.ID.Destination.String() == "0.0.0.0" && int(s.InetDiagMsg.State) == netlink.TCP_CLOSE {
			udpPort = fmt.Sprintf("%d", s.InetDiagMsg.ID.SourcePort)
			break
		}
	}
	if tcpPort == "" || udpPort == "" {
		return "", "", fmt.Errorf("could not find docker embedded resolver ports for %s (tcp=%q udp=%q)", loIP, tcpPort, udpPort)
	}
	return tcpPort, udpPort, nil
}

// ClearRules removes DNAT rules dockerRules could have added.
func clearRules() error {

	// This will be run from kill() so no memory state could be used.
	// we list all IPtable rules from scratch.

	// iptables-save output example:
	// -A PREROUTING -d 192.168.100.100/32 -i lo -p udp -j DNAT --to-destination 127.0.0.11:60269
	ipt, err := exec.LookPath("iptables-save")
	if err != nil {
		return err
	}
	out, err := exec.Command(ipt, "-t", "nat").Output() //nolint:gosec
	if err != nil {
		return fmt.Errorf("iptables-save failed: %w", err)
	}

	var retErr error
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "-A" || fields[1] != "PREROUTING" {
			continue
		}
		if !strings.Contains(line, "-i lo") || !strings.Contains(line, "-j DNAT") {
			continue
		}
		args := append([]string{"-t", "nat", "-D"}, fields[1:]...)
		if err := ipTablesExec(args); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}
	return retErr
}
