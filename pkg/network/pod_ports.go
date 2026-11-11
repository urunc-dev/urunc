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
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

const (
	// podPortRangePath holds the local port range of the network namespace
	// of the reading process, which is the one of the pod.
	podPortRangePath = "/proc/sys/net/ipv4/ip_local_port_range"
	// linuxGuestPortMax is the end of the default local port range of the
	// Linux kernel (32768-60999), which a Linux guest uses for its own
	// connections.
	linuxGuestPortMax = 60999
)

// podPortBand reads the local port range of the pod's network namespace
// and reports whether it can be kept for the pod's own connections.
func podPortBand() (uint16, uint16, bool) {
	data, err := os.ReadFile(podPortRangePath)
	if err != nil {
		netlog.Debugf("failed to read %s: %v", podPortRangePath, err)
		return 0, 0, false
	}
	return parsePortBand(string(data))
}

// parsePortBand parses a local port range ("low high"). The range is usable
// only when it is entirely above the local port range of a Linux guest, so
// that the ports of the guest's connections and services stay with the guest.
func parsePortBand(s string) (uint16, uint16, bool) {
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return 0, 0, false
	}
	low, err := strconv.ParseUint(fields[0], 10, 16)
	if err != nil {
		return 0, 0, false
	}
	high, err := strconv.ParseUint(fields[1], 10, 16)
	if err != nil {
		return 0, 0, false
	}
	if low <= linuxGuestPortMax || low > high {
		return 0, 0, false
	}
	return uint16(low), uint16(high), true
}

// addPodPortFilters adds filters on the ingress of eth that run before the
// redirect to the tap: ARP is copied to the tap and also passed to the pod,
// and TCP and UDP packets to the ports in [low, high] are passed to the pod.
// In that way, the other containers of the pod keep the replies to their own
// connections. If a filter can not be added, the ones already added are
// removed, leaving only the redirect.
func addPodPortFilters(eth netlink.Link, tap netlink.Link, low uint16, high uint16) error {
	tcp := nl.IPPROTO_TCP
	udp := nl.IPPROTO_UDP
	filters := []netlink.Filter{
		&netlink.Flower{
			FilterAttrs: podFilterAttrs(eth, 1, unix.ETH_P_ARP),
			EthType:     unix.ETH_P_ARP,
			Actions: []netlink.Action{
				&netlink.MirredAction{
					ActionAttrs: netlink.ActionAttrs{
						Action: netlink.TC_ACT_OK,
					},
					MirredAction: netlink.TCA_EGRESS_MIRROR,
					Ifindex:      tap.Attrs().Index,
				},
			},
		},
		podPortFilter(eth, 2, &tcp, low, high),
		podPortFilter(eth, 3, &udp, low, high),
	}

	var added []netlink.Filter
	for _, filter := range filters {
		err := netlink.FilterAdd(filter)
		if err != nil {
			var errs []error
			for _, f := range added {
				if delErr := netlink.FilterDel(f); delErr != nil {
					errs = append(errs, delErr)
				}
			}
			return errors.Join(fmt.Errorf("failed to add filter: %w", err), errors.Join(errs...))
		}
		added = append(added, filter)
	}

	return nil
}

func podFilterAttrs(link netlink.Link, priority uint16, protocol uint16) netlink.FilterAttrs {
	return netlink.FilterAttrs{
		LinkIndex: link.Attrs().Index,
		Parent:    netlink.MakeHandle(0xffff, 0),
		Priority:  priority,
		Protocol:  protocol,
	}
}

func podPortFilter(link netlink.Link, priority uint16, proto *nl.IPProto, low uint16, high uint16) *netlink.Flower {
	return &netlink.Flower{
		FilterAttrs:     podFilterAttrs(link, priority, unix.ETH_P_IP),
		EthType:         unix.ETH_P_IP,
		IPProto:         proto,
		DstPortRangeMin: low,
		DstPortRangeMax: high,
		Actions: []netlink.Action{
			&netlink.GenericAction{
				ActionAttrs: netlink.ActionAttrs{
					Action: netlink.TC_ACT_OK,
				},
			},
		},
	}
}
