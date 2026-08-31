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
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var lhlog = logrus.WithField("subsystem", "network")

// rules installs the custom part of the forwarding for some scenario.
type rules func(*Forwarder) error

type Forwarder struct {
	VirtIP     net.IP // virtual resolver IP the guest dials
	LoIP       net.IP // loopback IP the real resolver listens on
	ResolvConf string // host path of the container's resolv.conf
	custom     rules  // scenario-specific extra rules, picked by Detect
}

func Detect(resolvConf string, virtIP string) (*Forwarder, error) {
	if resolvConf == "" {
		return nil, nil
	}
	data, err := os.ReadFile(resolvConf)
	if err != nil {
		return nil, err
	}
	loIP := loNameserver(string(data))
	if loIP == nil {
		return nil, nil
	}

	ip := net.ParseIP(virtIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid virtual resolver IP %s", virtIP)
	}

	f := &Forwarder{
		VirtIP:     ip,
		LoIP:       loIP,
		ResolvConf: resolvConf,

		// now by default, if we detect localhost nameserver we apply dockerRules
		// which is the only case we cover.
		custom: dockerRules,
	}
	return f, nil
}

// RewriteResolvConf points the loopback nameservers in the container's
// resolv.conf at the virtIP.
func (f *Forwarder) RewriteResolvConf() error {
	data, err := os.ReadFile(f.ResolvConf)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", f.ResolvConf, err)
	}

	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))

	// changed is a flag for detect if we rewrite any nameserver
	changed := false
	for _, line := range lines {
		fields := strings.Fields(line)

		// if we have `resolv.conf` as:
		// nameserver 127.0.0.10
		// nameserver 127.0.0.20
		// nameserver 8.8.8.8
		// -------------------
		// our `loNameserver` function below will detect
		// and set our `loIP` as `127.0.0.10` (first loopback nameserver),
		// which will be the one in TC rules and Iptables one.
		// Knowing that we should only rewrite the first nameserver in our `resolv.conf`,
		// so final result will be:
		// nameserver virtIP
		// nameserver 127.0.0.20
		// nameserver 8.8.8.8
		// ```
		if !changed && len(fields) >= 2 && fields[0] == "nameserver" {
			if ip := net.ParseIP(fields[1]); ip != nil && ip.IsLoopback() {
				out = append(out, "nameserver "+f.VirtIP.String())
				changed = true
				continue
			}
		}
		out = append(out, line)
	}

	if err := os.WriteFile(f.ResolvConf, []byte(strings.Join(out, "\n")), 0o644); err != nil { //nolint: gosec
		return fmt.Errorf("failed to write %s: %w", f.ResolvConf, err)
	}
	return nil
}

// loNameserver returns the first loopback nameserver in resolv.conf data, if any.
func loNameserver(data string) net.IP {
	for line := range strings.SplitSeq(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			if ip := net.ParseIP(fields[1]); ip != nil && ip.IsLoopback() {
				return ip
			}
		}
	}
	return nil
}

// Apply installs the host side plumbing in the current netns:
// the general tc
// Kernel flags required
// the scenario's custom rules
func (f *Forwarder) Apply(tap netlink.Link, eth netlink.Link) error {
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return err
	}

	if err := addClsact(lo); err != nil {
		return fmt.Errorf("addClsact(lo) failed: %w", err)
	}
	if err := tapToLo(tap, lo, f.VirtIP); err != nil {
		return fmt.Errorf("tapToLo(%s) failed: %w", tap.Attrs().Name, err)
	}

	// The guest reuses the container's MAC, so replies must be addressed to it.
	if err := loToTap(lo, tap, f.VirtIP, eth.Attrs().HardwareAddr); err != nil {
		return fmt.Errorf("loToTap(%s) failed: %w", tap.Attrs().Name, err)
	}
	lhlog.Debug("Applied tc redirects between tap and lo")

	err = setSysctls(map[string]string{
		"/proc/sys/net/ipv4/conf/lo/route_localnet":                              "1",
		fmt.Sprintf("/proc/sys/net/ipv4/conf/%s/accept_local", tap.Attrs().Name): "1",
		"/proc/sys/net/ipv4/conf/lo/accept_local":                                "1",
	})
	if err != nil {
		return fmt.Errorf("failed to set required kernel flags for forwarding: %w", err)
	}
	lhlog.Debug("Applied kernel flags required for packet forwarding")

	return f.custom(f)
}

func dnat(virtIP net.IP, proto string, dst string) error {
	args := []string{
		"-t", "nat", "-A", "PREROUTING",
		"-i", "lo",
		"-d", virtIP.String(),
		"-p", proto,
		"-j", "DNAT",
		"--to-destination", dst,
		"--wait", "1",
	}
	if err := ipTablesExec(args); err != nil {
		return err
	}
	return nil
}

func addClsact(link netlink.Link) error {
	clsact := &netlink.Clsact{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.HANDLE_CLSACT,
		},
	}
	return netlink.QdiscAdd(clsact)
}

func tapToLo(tap netlink.Link, lo netlink.Link, virtIP net.IP) error {

	// tc filter add dev <tapX> ingress protocol ip pref x \
	//	    flower dst_ip <virtIP> \
	//	    action skbedit ptype host pipe \
	//	    action mirred ingress redirect dev lo
	return netlink.FilterAdd(&netlink.Flower{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: tap.Attrs().Index,
			Parent:    netlink.MakeHandle(0xffff, 0),
			Priority:  100,
			Protocol:  unix.ETH_P_IP,
		},
		DestIP: virtIP,
		Actions: []netlink.Action{
			&netlink.SkbEditAction{
				ActionAttrs: netlink.ActionAttrs{
					Action: netlink.TC_ACT_PIPE,
				},
				PType: uint16Ptr(unix.PACKET_HOST),
			}, &netlink.MirredAction{
				ActionAttrs: netlink.ActionAttrs{
					Action: netlink.TC_ACT_STOLEN,
				},
				MirredAction: netlink.TCA_INGRESS_REDIR,
				Ifindex:      lo.Attrs().Index,
			},
		},
	})
}

func loToTap(lo netlink.Link, tap netlink.Link, virtIP net.IP, guestMAC net.HardwareAddr) error {

	// tc filter add dev lo egress protocol ip pref x \
	//	    flower src_ip <virtIP> \
	//	    action pedit ex munge eth dst set <guestMAC> pipe \
	//	    action mirred egress redirect dev <tapX>
	return netlink.FilterAdd(&netlink.Flower{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: lo.Attrs().Index,
			Parent:    netlink.HANDLE_MIN_EGRESS,
			Priority:  100,
			Protocol:  unix.ETH_P_IP,
		},
		SrcIP: virtIP,
		Actions: []netlink.Action{
			&netlink.PeditAction{
				ActionAttrs: netlink.ActionAttrs{
					Action: netlink.TC_ACT_PIPE,
				},
				DstMacAddr: guestMAC,
			},
			&netlink.MirredAction{
				ActionAttrs: netlink.ActionAttrs{
					Action: netlink.TC_ACT_STOLEN,
				},
				MirredAction: netlink.TCA_EGRESS_REDIR,
				Ifindex:      tap.Attrs().Index,
			},
		},
	})
}

func setSysctls(flags map[string]string) error {
	for flag, value := range flags {
		file, err := os.OpenFile(flag, os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", flag, err)
		}
		defer file.Close()

		_, err = file.WriteString(value)
		if err != nil {
			return fmt.Errorf("failed to set flag %s to %s: %w", flag, value, err)
		}
	}
	return nil
}

// Cleanup removes the host side plumbing installed by Apply:
// - Del TC link
// - Del rules applied (e.g. iptables rules)
func Cleanup() error {
	var retErr error

	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("LinkByName(lo) failed: %w", err)
	}
	if err := deleteClsact(lo); err != nil {
		retErr = errors.Join(retErr, fmt.Errorf("failed to delete clsact qdisc on lo: %w", err))
	}
	if err := clearRules(); err != nil {
		retErr = errors.Join(retErr, fmt.Errorf("failed to flush DNAT rules: %w", err))
	}
	return retErr
}

func deleteClsact(link netlink.Link) error {
	qdiscs, err := netlink.QdiscList(link)
	if err != nil {
		return fmt.Errorf("QdiscList(%s) failed: %w", link.Attrs().Name, err)
	}
	for _, q := range qdiscs {
		if _, ok := q.(*netlink.Clsact); ok {
			if err := netlink.QdiscDel(q); err != nil {
				return fmt.Errorf("QdiscDel(clsact on %s) failed: %w", link.Attrs().Name, err)
			}
		}
	}
	return nil
}

func uint16Ptr(v uint16) *uint16 {
	return &v
}

func ipTablesExec(args []string) error {
	var stdout, stderr bytes.Buffer

	ipt, err := exec.LookPath("iptables")
	if err != nil {
		return err
	}
	args = append([]string{ipt}, args...)
	cmd := exec.Cmd{
		Path:   ipt,
		Args:   args,
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("iptables command %s failed: %s", cmd.String(), stderr.String())
		}
		return err
	}
	return nil
}
