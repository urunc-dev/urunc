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
	"net"
	"strings"

	"github.com/jackpal/gateway"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	DefaultTap = "tapX_urunc"
)

var netlog = logrus.WithField("subsystem", "network")

type UnikernelNetworkInfo struct {
	TapDevice string
	EthDevice Interface
}
type Manager interface {
	NetworkSetup(uid uint32, gid uint32) (*UnikernelNetworkInfo, error)
}

type Interface struct {
	IP             string
	DefaultGateway string
	Mask           string
	Interface      string
	MAC            string
}

func NewNetworkManager(networkType string) (Manager, error) {
	switch networkType {
	case "static":
		return &StaticNetwork{}, nil
	case "dynamic":
		return &DynamicNetwork{}, nil
	default:
		return nil, fmt.Errorf("network manager %s not supported", networkType)

	}
}

func getTapIndex() (int, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return 0, err
	}
	tapCount := 0
	for _, iface := range ifaces {
		if strings.Contains(iface.Name, "tap") {
			tapCount++
		}
	}
	if tapCount > 255 {
		return tapCount, fmt.Errorf("TAP interfaces count higher than 255")
	}
	return tapCount, nil
}

func createTapDevice(name string, mtu int, ownerUID, ownerGID uint32) (netlink.Link, error) {
	tapLinkAttrs := netlink.NewLinkAttrs()
	tapLinkAttrs.Name = name
	tapLink := &netlink.Tuntap{
		LinkAttrs: tapLinkAttrs,

		// We want a tap device (L2) as opposed to a tun (L3)
		Mode: netlink.TUNTAP_MODE_TAP,

		// Firecracker does not support multiqueue tap devices at this time:
		// https://github.com/firecracker-microvm/firecracker/issues/750
		Queues: 1,

		Flags: netlink.TUNTAP_ONE_QUEUE | // single queue tap device
			netlink.TUNTAP_VNET_HDR, // parse vnet headers added by the vm's virtio_net implementation
	}

	err := netlink.LinkAdd(tapLink)
	if err != nil {
		return nil, fmt.Errorf("failed to create tap device: %w", err)
	}

	for _, tapFd := range tapLink.Fds {
		err = unix.IoctlSetInt(int(tapFd.Fd()), unix.TUNSETOWNER, int(ownerUID))
		if err != nil {
			return nil, fmt.Errorf("failed to set tap %s owner to uid %d: %w", name, ownerUID, err)
		}

		err = unix.IoctlSetInt(int(tapFd.Fd()), unix.TUNSETGROUP, int(ownerGID))
		if err != nil {
			return nil, fmt.Errorf("failed to set tap %s group to gid %d: %w", name, ownerGID, err)
		}
	}

	err = netlink.LinkSetMTU(tapLink, mtu)
	if err != nil {
		return nil, fmt.Errorf("failed to set tap device MTU to %d: %w", mtu, err)
	}

	return tapLink, nil
}

func getInterfaceInfo(iface string) (Interface, error) {
	ief, err := net.InterfaceByName(iface)
	if err != nil {
		return Interface{}, err
	}
	IfMAC := ief.HardwareAddr.String()
	if IfMAC == "" {
		return Interface{}, fmt.Errorf("failed to get MAC address of %q", ief)
	}

	addrs, err := ief.Addrs()
	if err != nil {
		return Interface{}, err
	}
	ipAddress := ""
	mask := ""
	netMask := net.IPMask{}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			ipAddress = ipNet.IP.String()
			// hexadecimal notation
			mask = ipNet.Mask.String()
			netMask = ipNet.Mask
			break
		}
	}
	if mask == "" {
		return Interface{}, fmt.Errorf("failed to find mask for %q", iface)
	}
	// convert to decimal notation
	decimalParts := make([]string, len(netMask))
	for i, part := range netMask {
		decimalParts[i] = fmt.Sprintf("%d", part)
	}
	mask = strings.Join(decimalParts, ".")
	if ipAddress == "" {
		return Interface{}, fmt.Errorf("failed to find IPv4 address for %q", iface)
	}
	gateway, err := gateway.DiscoverGateway()
	if err != nil {
		return Interface{}, err
	}
	return Interface{
		IP:             ipAddress,
		DefaultGateway: gateway.String(),
		Mask:           mask,
		Interface:      iface,
		MAC:            IfMAC,
	}, nil
}

func addIngressQdisc(link netlink.Link) error {
	ingress := &netlink.Ingress{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.HANDLE_INGRESS,
		},
	}
	return netlink.QdiscAdd((ingress))
}

func addRedirectFilter(source netlink.Link, target netlink.Link) error {
	return netlink.FilterAdd(&netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: source.Attrs().Index,
			Parent:    netlink.MakeHandle(0xffff, 0),
			Protocol:  unix.ETH_P_ALL,
		},
		Actions: []netlink.Action{
			&netlink.MirredAction{
				ActionAttrs: netlink.ActionAttrs{
					Action: netlink.TC_ACT_STOLEN,
				},
				MirredAction: netlink.TCA_EGRESS_REDIR,
				Ifindex:      target.Attrs().Index,
			},
		},
	})
}

func networkSetup(tapName string, ipAddress string, redirectLink netlink.Link, addTCRules bool, uid uint32, gid uint32) (netlink.Link, error) {
	netlog.Debugf("starting for tapName=%s ipAddress=%s redirectLink=%s addTCRules=%v",
		tapName, ipAddress, redirectLink.Attrs().Name, addTCRules)
	// Create TAP
	netlog.Debugf("creating tap device %s (mtu=%d)", tapName, redirectLink.Attrs().MTU)
	newTapDevice, err := createTapDevice(tapName, redirectLink.Attrs().MTU, uid, gid)
	if err != nil {
		return nil, fmt.Errorf("createTapDevice(%s) failed: %w", tapName, err)
	}
	netlog.Debugf("created tap device %s (index=%d)", newTapDevice.Attrs().Name, newTapDevice.Attrs().Index)

	// Bring TAP up before qdisc
	if err = netlink.LinkSetUp(newTapDevice); err != nil {
		return nil, fmt.Errorf("LinkSetUp(%s) failed: %w", newTapDevice.Attrs().Name, err)
	}
	netlog.Debugf("TAP %s is UP", newTapDevice.Attrs().Name)

	// Bring redirectLink (eth0) up before using it in filters
	if err = netlink.LinkSetUp(redirectLink); err != nil {
		return nil, fmt.Errorf("LinkSetUp(%s) failed: %w", redirectLink.Attrs().Name, err)
	}
	netlog.Debugf("redirectLink %s is UP", redirectLink.Attrs().Name)

	// Add qdisc + redirect filters
	if addTCRules {
		netlog.Debug("adding tc ingress qdisc + redirect filters")

		if err = addIngressQdisc(newTapDevice); err != nil {
			return nil, fmt.Errorf("addIngressQdisc(tap=%s) failed: %w",
				newTapDevice.Attrs().Name, err)
		}
		if err = addIngressQdisc(redirectLink); err != nil {
			return nil, fmt.Errorf("addIngressQdisc(redirect=%s) failed: %w",
				redirectLink.Attrs().Name, err)
		}
		if err = addRedirectFilter(newTapDevice, redirectLink); err != nil {
			return nil, fmt.Errorf("addRedirectFilter(%s->%s) failed: %w",
				newTapDevice.Attrs().Name, redirectLink.Attrs().Name, err)
		}
		if err = addRedirectFilter(redirectLink, newTapDevice); err != nil {
			return nil, fmt.Errorf("addRedirectFilter(%s->%s) failed: %w",
				redirectLink.Attrs().Name, newTapDevice.Attrs().Name, err)
		}
	}

	// Optionally assign IP to TAP
	if ipAddress != "" {
		netlog.Debugf("assigning IP %s to %s", ipAddress, newTapDevice.Attrs().Name)
		ipn, err := netlink.ParseAddr(ipAddress)
		if err != nil {
			return nil, fmt.Errorf("ParseAddr(%s) failed: %w", ipAddress, err)
		}
		if err = netlink.AddrReplace(newTapDevice, ipn); err != nil {
			return nil, fmt.Errorf("AddrReplace(%s, %s) failed: %w",
				newTapDevice.Attrs().Name, ipAddress, err)
		}
	}

	netlog.Debugf("completed successfully (tap=%s, index=%d)", newTapDevice.Attrs().Name, newTapDevice.Attrs().Index)
	return newTapDevice, nil
}

func Cleanup(tapDevice string) error {
	netlog.Debug("net cleanup called")
	ifaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	for _, iface := range ifaces {
		netlog.Debugf("Discovered device %s", iface.Name)
	}
	tapLink, err := netlink.LinkByName(tapDevice)
	if err != nil {
		netlog.Errorf("Failed to get link %s by name: %v", tapDevice, err)
		return nil
	}
	err = deleteAllTCFilters(tapLink)
	if err != nil {
		netlog.Errorf("Failed to delete all TC filters: %v", err)
		return err
	}
	err = deleteAllQDiscs(tapLink)
	if err != nil {
		netlog.Errorf("Failed to delete all qdiscs: %v", err)
		return err
	}
	err = deleteTapDevice(tapLink)
	if err != nil {
		netlog.Errorf("Failed to delete link %s: %v", tapDevice, err)
	}
	return nil
}

func deleteIngressQdisc(link netlink.Link) error {
	qdiscs, err := netlink.QdiscList(link)
	if err != nil {
		return err
	}
	for _, qdisc := range qdiscs {
		if qdisc.Attrs().Parent == netlink.HANDLE_INGRESS && qdisc.Attrs().LinkIndex == link.Attrs().Index {
			err = netlink.QdiscDel(qdisc)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// discoverContainerIface discovers the container's network interface by
// scanning all links in the current network namespace and returning the first
// non-loopback link that has an IP address and a default route.
func discoverContainerIface() (netlink.Link, error) {
	handle, err := netlink.NewHandle()
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	links, err := handle.LinkList()
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		attrs := link.Attrs()
		if attrs == nil {
			netlog.Debug("skipping link with nil attributes")
			continue
		}
		// skip loopback
		if (attrs.Flags & net.FlagLoopback) != 0 {
			netlog.Debugf("skipping loopback interface %s", attrs.Name)
			continue
		}
		// must have addresses configured
		addrs, err := handle.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			netlog.Debugf("skipping interface %s: failed to list addresses: %v", attrs.Name, err)
			continue
		}
		if len(addrs) == 0 {
			netlog.Debugf("skipping interface %s: no addresses configured", attrs.Name)
			continue
		}
		// look for a default route on this interface
		routes, err := handle.RouteList(link, netlink.FAMILY_ALL)
		if err != nil {
			netlog.Debugf("skipping interface %s: failed to list routes: %v", attrs.Name, err)
			continue
		}
		for _, r := range routes {
			// default route: Dst == nil OR represented as 0.0.0.0/0 or ::/0
			if r.Dst == nil {
				return link, nil
			}
			if r.Dst != nil {
				dstStr := r.Dst.String()
				if dstStr == "0.0.0.0/0" || dstStr == "::/0" {
					return link, nil
				}
			}
		}
		netlog.Debugf("skipping interface %s: no default route found", attrs.Name)
	}
	return nil, errors.New("no suitable network interface found in namespace")
}

func deleteAllQDiscs(device netlink.Link) error {
	err := deleteIngressQdisc(device)
	if err != nil {
		return err
	}
	device, err = discoverContainerIface()
	if err != nil {
		return err
	}
	err = deleteIngressQdisc(device)
	if err != nil {
		return err
	}
	return nil
}

func deleteAllTCFilters(device netlink.Link) error {
	var allFilters []netlink.Filter
	parent := uint32(netlink.HANDLE_ROOT)
	tapFilters, err := netlink.FilterList(device, parent)
	if err != nil {
		return err
	}
	allFilters = append(allFilters, tapFilters...)
	device, err = discoverContainerIface()
	if err != nil {
		return err
	}
	ethFilters, err := netlink.FilterList(device, parent)
	if err != nil {
		return err
	}
	allFilters = append(allFilters, ethFilters...)
	for _, filter := range allFilters {
		err = netlink.FilterDel(filter)
		if err != nil {
			return err
		}
	}
	return nil
}

func deleteTapDevice(device netlink.Link) error {
	err := netlink.LinkSetDown(device)
	if err != nil {
		netlog.Errorf("Failed to set link down: %v", err)
		return err
	}
	err = netlink.LinkDel(device)
	if err != nil {
		netlog.Errorf("Failed to delete link: %v", err)
		return err
	}
	return nil
}
