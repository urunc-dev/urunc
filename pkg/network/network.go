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
	"regexp"
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

// ErrNoContainerIface is returned when the network namespace has no
// non-loopback interface with an address and a default route. That is the
// one network setup failure a caller may tolerate: the container was started
// without a network (ctr, for instance), so the guest boots without a network
// device. Every other failure is a different error.
var ErrNoContainerIface = errors.New("no suitable network interface found in namespace")

// netlinkHandle is the part of *netlink.Handle that interface discovery
// uses. It exists so tests can hand discovery a fake instead of a socket.
type netlinkHandle interface {
	LinkList() ([]netlink.Link, error)
	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
	RouteList(link netlink.Link, family int) ([]netlink.Route, error)
	Close()
}

// newNetlinkHandle opens a netlink handle for the given socket families.
// Tests replace it to record the families and return a fake.
var newNetlinkHandle = func(nlFamilies ...int) (netlinkHandle, error) {
	h, err := netlink.NewHandle(nlFamilies...)
	if err != nil {
		return nil, err
	}
	return h, nil
}

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
	MTU            int
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

// netInterfacesFn is the seam the setup tests replace.
var netInterfacesFn = net.Interfaces

func getTapIndex() (int, error) {
	ifaces, err := netInterfacesFn()
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

	// The tap exists from here on, so a failure removes it again.
	fail := func(err error) (netlink.Link, error) {
		if delErr := deleteTapDevice(tapLink); delErr != nil {
			netlog.Warnf("failed to remove tap %s after a failed setup: %v", name, delErr)
		}
		return nil, err
	}

	// LinkAdd opened the tap device's fd and set TUNSETPERSIST, so the
	// interface survives independently of any open fd. Nothing after the
	// owner/group ioctl calls uses the fd (the remaining setup goes through
	// netlink and the monitor attaches to the device by name with its own
	// open), so close it right away instead of relying on a later exec to
	// close it (O_CLOEXEC). A single-queue tap device only allows one
	// attached fd at a time, so holding it open blocks the monitor from
	// attaching if the caller does not exec away.
	for _, tapFd := range tapLink.Fds {
		err = unix.IoctlSetInt(int(tapFd.Fd()), unix.TUNSETOWNER, int(ownerUID))
		if err != nil {
			if closeErr := tapFd.Close(); closeErr != nil {
				netlog.Warnf("failed to close tap %s fd after owner ioctl error: %v", name, closeErr)
			}
			return fail(fmt.Errorf("failed to set tap %s owner to uid %d: %w", name, ownerUID, err))
		}

		err = unix.IoctlSetInt(int(tapFd.Fd()), unix.TUNSETGROUP, int(ownerGID))
		if err != nil {
			if closeErr := tapFd.Close(); closeErr != nil {
				netlog.Warnf("failed to close tap %s fd after group ioctl error: %v", name, closeErr)
			}
			return fail(fmt.Errorf("failed to set tap %s group to gid %d: %w", name, ownerGID, err))
		}

		err = tapFd.Close()
		if err != nil {
			return fail(fmt.Errorf("failed to close the fd of tap %s: %w", name, err))
		}
	}

	err = netlink.LinkSetMTU(tapLink, mtu)
	if err != nil {
		return fail(fmt.Errorf("failed to set tap device MTU to %d: %w", mtu, err))
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
		return Interface{}, fmt.Errorf("failed to get MAC address of %v", ief)
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
		MTU:            ief.MTU,
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

func networkSetup(tapName string, ipAddress string, redirectLink netlink.Link, addTCRules bool, uid uint32, gid uint32) (netlink.Link, error) {
	netlog.Debugf("starting for tapName=%s ipAddress=%s redirectLink=%s addTCRules=%v",
		tapName, ipAddress, redirectLink.Attrs().Name, addTCRules)
	// Create TAP
	netlog.Debugf("creating tap device %s (mtu=%d)", tapName, redirectLink.Attrs().MTU)
	newTapDevice, err := createTapDeviceFn(tapName, redirectLink.Attrs().MTU, uid, gid)
	if err != nil {
		return nil, fmt.Errorf("createTapDevice(%s) failed: %w", tapName, err)
	}
	netlog.Debugf("created tap device %s (index=%d)", newTapDevice.Attrs().Name, newTapDevice.Attrs().Index)

	// ethQdiscAdded records whether this setup added the ingress qdisc on
	// redirectLink, so a rollback removes it only then.
	ethQdiscAdded := false
	fail := func(err error) (netlink.Link, error) {
		rollbackTap(newTapDevice, redirectLink, ethQdiscAdded)
		return nil, err
	}

	// Bring TAP up before qdisc
	if err = linkSetUpFn(newTapDevice); err != nil {
		return fail(fmt.Errorf("LinkSetUp(%s) failed: %w", newTapDevice.Attrs().Name, err))
	}
	netlog.Debugf("TAP %s is UP", newTapDevice.Attrs().Name)

	// Bring redirectLink (eth0) up before using it in filters
	if err = linkSetUpFn(redirectLink); err != nil {
		return fail(fmt.Errorf("LinkSetUp(%s) failed: %w", redirectLink.Attrs().Name, err))
	}
	netlog.Debugf("redirectLink %s is UP", redirectLink.Attrs().Name)

	// Add qdisc + redirect filters
	if addTCRules {
		netlog.Debug("adding tc ingress qdisc + redirect filters")

		if err = addIngressQdiscFn(newTapDevice); err != nil {
			return fail(fmt.Errorf("addIngressQdisc(tap=%s) failed: %w",
				newTapDevice.Attrs().Name, err))
		}
		if err = addIngressQdiscFn(redirectLink); err != nil {
			return fail(fmt.Errorf("addIngressQdisc(redirect=%s) failed: %w",
				redirectLink.Attrs().Name, err))
		}
		ethQdiscAdded = true
		if _, err = addRedirectFilters(newTapDevice, redirectLink, redirectClassifiers); err != nil {
			return fail(err)
		}
	}

	// Optionally assign IP to TAP
	if ipAddress != "" {
		netlog.Debugf("assigning IP %s to %s", ipAddress, newTapDevice.Attrs().Name)
		ipn, err := netlink.ParseAddr(ipAddress)
		if err != nil {
			return fail(fmt.Errorf("ParseAddr(%s) failed: %w", ipAddress, err))
		}
		if err = addrReplaceFn(newTapDevice, ipn); err != nil {
			return fail(fmt.Errorf("AddrReplace(%s, %s) failed: %w",
				newTapDevice.Attrs().Name, ipAddress, err))
		}
	}

	netlog.Debugf("completed successfully (tap=%s, index=%d)", newTapDevice.Attrs().Name, newTapDevice.Attrs().Index)
	return newTapDevice, nil
}

// CleanupAllUruncTaps removes every tapN_urunc device in the current network
// namespace. This is the blanket cleanup of the stop path. For each tap it
// deletes every filter at HANDLE_ROOT and the ingress qdisc on both the tap
// and the container interface, and then the tap itself. rollbackTap is
// different: it undoes only what a failed setup created. When the namespace
// has no container interface, only the tap side is cleaned.
func CleanupAllUruncTaps() error {
	netlog.Debug("net cleanup called")

	handle, err := newNetlinkHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return fmt.Errorf("failed to open a netlink route handle: %w", err)
	}
	defer handle.Close()

	links, err := handle.LinkList()
	if err != nil {
		return fmt.Errorf("failed to list links: %w", err)
	}

	eth, err := selectContainerIface(handle, links)
	if err != nil {
		netlog.Debugf("no usable container interface, cleaning tap side only: %v", err)
		eth = nil
	}

	var retErr error
	tapRe := regexp.MustCompile(`^tap\d+_urunc$`)
	for _, link := range links {
		attrs := link.Attrs()
		if attrs == nil {
			continue
		}
		name := attrs.Name
		if !tapRe.MatchString(name) {
			continue
		}

		netlog.Debugf("cleaning up tap device %s", name)
		var devErr error
		if err := deleteAllTCFiltersFn(link, eth); err != nil {
			netlog.Errorf("failed to delete TC filters for %s: %v", name, err)
			devErr = errors.Join(devErr, err)
		}
		if err := deleteAllQDiscsFn(link, eth); err != nil {
			netlog.Errorf("failed to delete qdiscs for %s: %v", name, err)
			devErr = errors.Join(devErr, err)
		}
		if err := deleteTapDeviceFn(link); err != nil {
			netlog.Errorf("failed to delete tap %s: %v", name, err)
			devErr = errors.Join(devErr, err)
		}
		if devErr == nil {
			netlog.Debugf("deleted tap device %s", name)
		}
		retErr = errors.Join(retErr, devErr)
	}

	return retErr
}

// Seams for the setup, rollback and cleanup tests.
var (
	createTapDeviceFn    = createTapDevice
	linkSetUpFn          = netlink.LinkSetUp
	addIngressQdiscFn    = addIngressQdisc
	addrReplaceFn        = netlink.AddrReplace
	deleteIngressQdiscFn = deleteIngressQdisc
	deleteTapDeviceFn    = deleteTapDevice
	deleteAllTCFiltersFn = deleteAllTCFilters
	deleteAllQDiscsFn    = deleteAllQDiscs
)

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

// rollbackTap undoes what a failed network setup created, and nothing else:
// the ingress qdisc on eth only when this setup added it (deleting it takes
// the eth->tap filter with it and leaves anything a CNI plugin installed on
// eth alone), then the tap, whose qdisc and filter go with the device. It is
// best effort: failures are logged and the caller returns its original error.
func rollbackTap(tap netlink.Link, eth netlink.Link, ethQdiscAdded bool) {
	name := tap.Attrs().Name
	if ethQdiscAdded && eth != nil {
		if err := deleteIngressQdiscFn(eth); err != nil {
			netlog.Warnf("rollback of %s after a failed network setup: %v", name, err)
		}
	}
	if err := deleteTapDeviceFn(tap); err != nil {
		netlog.Warnf("rollback of %s after a failed network setup: %v", name, err)
	}
}

// discoverContainerIface discovers the container's network interface by
// scanning all links in the current network namespace and returning the first
// non-loopback link that has an IP address and a default route. It returns
// ErrNoContainerIface when no link qualifies, and a different error when the
// links cannot be listed.
func discoverContainerIface() (netlink.Link, error) {
	// Discovery needs only a route socket. A handle with no families also
	// opens `XFRM` and `NETFILTER` sockets, and a kernel without one of them
	// refuses the whole handle with "protocol not supported".
	handle, err := newNetlinkHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, fmt.Errorf("failed to open a netlink route handle: %w", err)
	}
	defer handle.Close()
	links, err := handle.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list links: %w", err)
	}
	return selectContainerIface(handle, links)
}

// selectContainerIface returns the first link in links that is not a
// loopback, has an address and has a default route, asking handle for the
// addresses and routes. It returns ErrNoContainerIface when none qualifies,
// and a different error when a link cannot be inspected, so a netlink failure
// is never taken for a container without an interface.
func selectContainerIface(handle netlinkHandle, links []netlink.Link) (netlink.Link, error) {
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
		// A dump interrupted by a concurrent change still returns usable
		// results.
		if err != nil && !errors.Is(err, netlink.ErrDumpInterrupted) {
			return nil, fmt.Errorf("failed to list addresses of %s: %w", attrs.Name, err)
		}
		if len(addrs) == 0 {
			netlog.Debugf("skipping interface %s: no addresses configured", attrs.Name)
			continue
		}
		// look for a default route on this interface
		routes, err := handle.RouteList(link, netlink.FAMILY_ALL)
		if err != nil && !errors.Is(err, netlink.ErrDumpInterrupted) {
			return nil, fmt.Errorf("failed to list routes of %s: %w", attrs.Name, err)
		}
		for _, r := range routes {
			// default route: Dst == nil OR represented as 0.0.0.0/0 or ::/0
			if r.Dst == nil {
				return link, nil
			}
			dstStr := r.Dst.String()
			if dstStr == "0.0.0.0/0" || dstStr == "::/0" {
				return link, nil
			}
		}
		netlog.Debugf("skipping interface %s: no default route found", attrs.Name)
	}
	return nil, ErrNoContainerIface
}

// deleteAllQDiscs deletes the ingress qdisc on device and, when eth is not
// nil, on eth.
func deleteAllQDiscs(device, eth netlink.Link) error {
	if err := deleteIngressQdisc(device); err != nil {
		return err
	}
	if eth == nil {
		return nil
	}
	return deleteIngressQdisc(eth)
}

// deleteAllTCFilters deletes every filter at HANDLE_ROOT on device and, when
// eth is not nil, on eth.
func deleteAllTCFilters(device, eth netlink.Link) error {
	parent := uint32(netlink.HANDLE_ROOT)
	allFilters, err := netlink.FilterList(device, parent)
	if err != nil {
		return err
	}
	if eth != nil {
		var ethFilters []netlink.Filter
		ethFilters, err = netlink.FilterList(eth, parent)
		if err != nil {
			return err
		}
		allFilters = append(allFilters, ethFilters...)
	}
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
