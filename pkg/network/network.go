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

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	DefaultTap                 = "tapX_urunc"
	ingressFilterParent uint32 = 0xffff0000
)

var netlog = logrus.WithField("subsystem", "network")
var uruncTapName = regexp.MustCompile(`^tap\d+_urunc$`)

// ErrNoContainerIface reports a network namespace with no usable interface,
// as in a container started with no network.
var ErrNoContainerIface = errors.New("no suitable network interface found in namespace")

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

func createTapDevice(name string, mtu int, ownerUID, ownerGID uint32) (_ netlink.Link, retErr error) {
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
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, deleteTapDevice(tapLink))
		}
	}()

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
			return nil, fmt.Errorf("failed to set tap %s owner to uid %d: %w", name, ownerUID, err)
		}

		err = unix.IoctlSetInt(int(tapFd.Fd()), unix.TUNSETGROUP, int(ownerGID))
		if err != nil {
			if closeErr := tapFd.Close(); closeErr != nil {
				netlog.Warnf("failed to close tap %s fd after group ioctl error: %v", name, closeErr)
			}
			return nil, fmt.Errorf("failed to set tap %s group to gid %d: %w", name, ownerGID, err)
		}

		err = tapFd.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to close the fd of tap %s: %w", name, err)
		}
	}

	err = netlink.LinkSetMTU(tapLink, mtu)
	if err != nil {
		return nil, fmt.Errorf("failed to set tap device MTU to %d: %w", mtu, err)
	}

	return tapLink, nil
}

func ingressQdisc(link netlink.Link) *netlink.Ingress {
	return &netlink.Ingress{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    ingressFilterParent,
			Parent:    netlink.HANDLE_INGRESS,
		},
	}
}

func redirectFilter(source netlink.Link, target netlink.Link) *netlink.U32 {
	return &netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: source.Attrs().Index,
			Parent:    ingressFilterParent,
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
	}
}

type tapRedirectSetup interface {
	QdiscAdd(netlink.Qdisc) error
	QdiscDel(netlink.Qdisc) error
	FilterAdd(netlink.Filter) error
}

func setupTapRedirect(handle tapRedirectSetup, tap, peer netlink.Link) (retErr error) {
	tapName, peerName := tap.Attrs().Name, peer.Attrs().Name
	if err := handle.QdiscAdd(ingressQdisc(tap)); err != nil {
		return fmt.Errorf("addIngressQdisc(tap=%s) failed: %w", tapName, err)
	}
	peerIngress := ingressQdisc(peer)
	if err := handle.QdiscAdd(peerIngress); err != nil {
		return fmt.Errorf("addIngressQdisc(redirect=%s) failed: %w", peerName, err)
	}
	// The peer qdisc was created exclusively by this attempt. A failed
	// redirect must release it so a restart can configure the interface.
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, handle.QdiscDel(peerIngress))
		}
	}()
	if err := handle.FilterAdd(redirectFilter(tap, peer)); err != nil {
		return fmt.Errorf("addRedirectFilter(%s->%s) failed: %w", tapName, peerName, err)
	}
	if err := handle.FilterAdd(redirectFilter(peer, tap)); err != nil {
		return fmt.Errorf("addRedirectFilter(%s->%s) failed: %w", peerName, tapName, err)
	}
	return nil
}

func networkSetup(tapName string, ipAddress string, redirectLink netlink.Link, addTCRules bool, uid uint32, gid uint32) (_ netlink.Link, retErr error) {
	netlog.Debugf("starting for tapName=%s ipAddress=%s redirectLink=%s addTCRules=%v",
		tapName, ipAddress, redirectLink.Attrs().Name, addTCRules)
	// Create TAP
	netlog.Debugf("creating tap device %s (mtu=%d)", tapName, redirectLink.Attrs().MTU)
	newTapDevice, err := createTapDevice(tapName, redirectLink.Attrs().MTU, uid, gid)
	if err != nil {
		return nil, fmt.Errorf("createTapDevice(%s) failed: %w", tapName, err)
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, deleteTapDevice(newTapDevice))
		}
	}()
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

	if addTCRules {
		netlog.Debug("adding tc ingress qdisc + redirect filters")
		handle, err := netlink.NewHandle(unix.NETLINK_ROUTE)
		if err != nil {
			return nil, fmt.Errorf("failed to open netlink handle for ingress redirects: %w", err)
		}
		defer handle.Close()
		if err = setupTapRedirect(handle, newTapDevice, redirectLink); err != nil {
			return nil, err
		}
	}

	netlog.Debugf("completed successfully (tap=%s, index=%d)", newTapDevice.Attrs().Name, newTapDevice.Attrs().Index)
	return newTapDevice, nil
}

func CleanupAllUruncTaps() error {
	netlog.Debug("net cleanup called")

	handle, err := netlink.NewHandle()
	if err != nil {
		return fmt.Errorf("failed to get netlink handle: %w", err)
	}
	defer handle.Close()

	links, err := handle.LinkList()
	if err != nil {
		return fmt.Errorf("failed to list links: %w", err)
	}

	var retErr error
	for _, link := range links {
		attrs := link.Attrs()
		if attrs == nil {
			continue
		}
		name := attrs.Name
		if !uruncTapName.MatchString(name) {
			continue
		}

		netlog.Debugf("cleaning up tap device %s", name)
		var devErr error
		if err := cleanupTapRedirect(handle, link, links); err != nil {
			netlog.Errorf("failed to clean up the ingress redirect for %s: %v", name, err)
			devErr = errors.Join(devErr, err)
		}
		if err := deleteTapDevice(link); err != nil {
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

type tapRedirectCleanup interface {
	FilterList(netlink.Link, uint32) ([]netlink.Filter, error)
	FilterDel(netlink.Filter) error
	QdiscList(netlink.Link) ([]netlink.Qdisc, error)
	QdiscDel(netlink.Qdisc) error
}

// The TAP's redirect identifies the original peer even if the CNI routes
// changed. A static TAP has no redirect, so it has no peer rules to remove.
func cleanupTapRedirect(handle tapRedirectCleanup, tap netlink.Link, links []netlink.Link) error {
	filters, err := handle.FilterList(tap, ingressFilterParent)
	if err != nil {
		return err
	}
	for _, filter := range filters {
		peerIndex := ingressRedirectTarget(filter)
		if peerIndex == 0 {
			continue
		}
		for _, peer := range links {
			if attrs := peer.Attrs(); attrs != nil && attrs.Index == peerIndex {
				return removePeerRedirect(handle, peer, tap.Attrs().Index)
			}
		}
	}
	return nil
}

func ingressRedirectTarget(filter netlink.Filter) int {
	u32, ok := filter.(*netlink.U32)
	if !ok || u32.Protocol != unix.ETH_P_ALL || len(u32.Actions) != 1 {
		return 0
	}
	redirect, ok := u32.Actions[0].(*netlink.MirredAction)
	if !ok || redirect.MirredAction != netlink.TCA_EGRESS_REDIR || redirect.Action != netlink.TC_ACT_STOLEN {
		return 0
	}
	return redirect.Ifindex
}

func removePeerRedirect(handle tapRedirectCleanup, peer netlink.Link, tapIndex int) error {
	filters, err := handle.FilterList(peer, ingressFilterParent)
	if err != nil {
		return err
	}
	var redirects []netlink.Filter
	for _, filter := range filters {
		if ingressRedirectTarget(filter) == tapIndex {
			redirects = append(redirects, filter)
		}
	}
	if len(redirects) == 0 {
		return nil
	}
	// Setup creates an ingress qdisc exclusively. Remove it for a restart
	// when all its filters belong to the TAP, and preserve any other rules.
	if len(redirects) == len(filters) {
		qdiscs, err := handle.QdiscList(peer)
		if err != nil {
			return err
		}
		for _, qdisc := range qdiscs {
			if _, ok := qdisc.(*netlink.Ingress); ok {
				return handle.QdiscDel(qdisc)
			}
		}
	}
	var retErr error
	for _, filter := range redirects {
		retErr = errors.Join(retErr, handle.FilterDel(filter))
	}
	return retErr
}

// discoverContainerIface finds an IPv4 interface in the current namespace.
// A default route is preferred, but an internal bridge can have local
// connectivity without a default route.
func discoverContainerIface() (netlink.Link, Interface, error) {
	handle, err := netlink.NewHandle()
	if err != nil {
		return nil, Interface{}, err
	}
	defer handle.Close()
	links, err := handle.LinkList()
	if err != nil {
		return nil, Interface{}, err
	}
	return selectContainerIface(handle, links)
}

type linkAddressesAndRoutes interface {
	AddrList(netlink.Link, int) ([]netlink.Addr, error)
	RouteList(netlink.Link, int) ([]netlink.Route, error)
}

func selectContainerIface(handle linkAddressesAndRoutes, links []netlink.Link) (netlink.Link, Interface, error) {
	var routedLink netlink.Link
	var routedInfo Interface
	var routePriority int
	var fallbackLink netlink.Link
	var fallbackInfo Interface
	for _, link := range links {
		attrs := link.Attrs()
		if attrs == nil || attrs.Flags&net.FlagLoopback != 0 || len(attrs.HardwareAddr) == 0 || uruncTapName.MatchString(attrs.Name) {
			continue
		}
		addrs, err := handle.AddrList(link, netlink.FAMILY_V4)
		if err != nil {
			netlog.Debugf("skipping interface %s: failed to list addresses: %v", attrs.Name, err)
			continue
		}
		var address *net.IPNet
		for _, addr := range addrs {
			if addr.IPNet != nil && addr.IP.To4() != nil && !addr.IP.IsLoopback() {
				address = addr.IPNet
				break
			}
		}
		if address == nil {
			continue
		}
		routes, err := handle.RouteList(link, netlink.FAMILY_V4)
		if err != nil {
			netlog.Debugf("skipping interface %s: failed to list routes: %v", attrs.Name, err)
			continue
		}
		info := Interface{
			IP:        address.IP.String(),
			Mask:      net.IP(address.Mask).String(),
			Interface: attrs.Name,
			MAC:       attrs.HardwareAddr.String(),
			MTU:       attrs.MTU,
		}
		for _, r := range routes {
			if r.Dst != nil && r.Dst.String() != "0.0.0.0/0" {
				// A connected route lets a local-only guest reach its subnet.
				if fallbackLink == nil && r.Gw == nil && r.Dst.Contains(address.IP) {
					fallbackLink, fallbackInfo = link, info
				}
				continue
			}
			if routedLink != nil && r.Priority >= routePriority {
				continue
			}
			routedLink, routedInfo, routePriority = link, info, r.Priority
			if r.Gw.To4() != nil && !r.Gw.IsUnspecified() {
				routedInfo.DefaultGateway = r.Gw.String()
			}
		}
	}
	if routedLink != nil {
		return routedLink, routedInfo, nil
	}
	if fallbackLink != nil {
		return fallbackLink, fallbackInfo, nil
	}
	return nil, Interface{}, ErrNoContainerIface
}

// ContainerIface describes the interface of the container in the current
// network namespace, without touching it, for a monitor that gets no tap:
// ErrNoContainerIface when there is none.
func ContainerIface() (Interface, error) {
	_, info, err := discoverContainerIface()
	return info, err
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
