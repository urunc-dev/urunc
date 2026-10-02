//go:build linux

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
	"net"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

// inPodNetns runs fn in a fresh network namespace holding what CNI gives a
// pod: an eth0 with an address and a default route through it.
func inPodNetns(t *testing.T, fn func(eth netlink.Link)) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root to create network namespaces and tap devices")
	}
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		t.Skip("needs /dev/net/tun")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	orig, err := netns.Get()
	require.NoError(t, err)
	defer orig.Close()
	ns, err := netns.New()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, netns.Set(orig))
		ns.Close()
	}()

	attrs := netlink.NewLinkAttrs()
	attrs.Name = "eth0"
	attrs.MTU = 1450
	require.NoError(t, netlink.LinkAdd(&netlink.Dummy{LinkAttrs: attrs}))
	eth, err := netlink.LinkByName("eth0")
	require.NoError(t, err)
	require.NoError(t, netlink.LinkSetUp(eth))
	addr, err := netlink.ParseAddr("10.88.0.2/24")
	require.NoError(t, err)
	require.NoError(t, netlink.AddrAdd(eth, addr))
	require.NoError(t, netlink.RouteAdd(&netlink.Route{LinkIndex: eth.Attrs().Index, Gw: net.ParseIP("10.88.0.1")}))

	fn(eth)
}

// redirectTargets lists the interfaces the ingress filters on link redirect to.
func redirectTargets(t *testing.T, link netlink.Link) []int {
	t.Helper()
	filters, err := netlink.FilterList(link, netlink.MakeHandle(0xffff, 0))
	require.NoError(t, err)
	var targets []int
	for _, f := range filters {
		u32, ok := f.(*netlink.U32)
		if !ok {
			continue
		}
		for _, a := range u32.Actions {
			if m, ok := a.(*netlink.MirredAction); ok {
				targets = append(targets, m.Ifindex)
			}
		}
	}
	return targets
}

// attachTap opens a queue on the tap, as a running monitor holds it.
func attachTap(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	require.NoError(t, err)
	ifr, err := unix.NewIfreq(name)
	require.NoError(t, err)
	ifr.SetUint16(unix.IFF_TAP | unix.IFF_NO_PI | unix.IFF_VNET_HDR | unix.IFF_ONE_QUEUE)
	require.NoError(t, unix.IoctlIfreq(int(f.Fd()), unix.TUNSETIFF, ifr))
	return f
}

// The first container's monitor exited on its own, so nothing tore down its
// tap. The container restarted in the same pod must still get the network.
func TestNetworkSetupAfterMonitorExit(t *testing.T) {
	inPodNetns(t, func(eth netlink.Link) {
		_, err := DynamicNetwork{}.NetworkSetup(0, 0)
		require.NoError(t, err)

		info, err := DynamicNetwork{}.NetworkSetup(0, 0)
		require.NoError(t, err, "a tap left by an exited monitor refused the restart")
		require.Equal(t, "tap0_urunc", info.TapDevice)

		tap, err := netlink.LinkByName(info.TapDevice)
		require.NoError(t, err)
		require.Equal(t, []int{tap.Attrs().Index}, redirectTargets(t, eth),
			"eth0 must redirect only to the new tap")
	})
}

// A tap a monitor still holds belongs to a running container, so the
// namespace stays taken and the second setup is refused as before.
func TestNetworkSetupKeepsATapInUse(t *testing.T) {
	inPodNetns(t, func(eth netlink.Link) {
		info, err := DynamicNetwork{}.NetworkSetup(0, 0)
		require.NoError(t, err)
		f := attachTap(t, info.TapDevice)
		defer f.Close()

		_, err = DynamicNetwork{}.NetworkSetup(0, 0)
		require.ErrorContains(t, err, "multiple unikernels")

		tap, err := netlink.LinkByName(info.TapDevice)
		require.NoError(t, err, "the tap in use was removed")
		require.Equal(t, []int{tap.Attrs().Index}, redirectTargets(t, eth))
	})
}

// A redirect on eth0 whose tap is already gone must not block the next setup.
func TestNetworkSetupAfterTapRemovedAlone(t *testing.T) {
	inPodNetns(t, func(eth netlink.Link) {
		info, err := DynamicNetwork{}.NetworkSetup(0, 0)
		require.NoError(t, err)
		tap, err := netlink.LinkByName(info.TapDevice)
		require.NoError(t, err)
		require.NoError(t, netlink.LinkDel(tap))

		info, err = DynamicNetwork{}.NetworkSetup(0, 0)
		require.NoError(t, err, "a leftover ingress qdisc on eth0 blocked the setup")
		tap, err = netlink.LinkByName(info.TapDevice)
		require.NoError(t, err)
		require.Equal(t, []int{tap.Attrs().Index}, redirectTargets(t, eth))
	})
}
