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
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// fakeHandle is a netlinkHandle backed by maps keyed by link index, so
// discovery can run without a netlink socket.
type fakeHandle struct {
	links    []netlink.Link
	addrs    map[int][]netlink.Addr
	routes   map[int][]netlink.Route
	listErr  error
	addrErr  error
	routeErr error
	closed   bool
}

func (f *fakeHandle) LinkList() ([]netlink.Link, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.links, nil
}

func (f *fakeHandle) AddrList(link netlink.Link, _ int) ([]netlink.Addr, error) {
	return f.addrs[link.Attrs().Index], f.addrErr
}

func (f *fakeHandle) RouteList(link netlink.Link, _ int) ([]netlink.Route, error) {
	return f.routes[link.Attrs().Index], f.routeErr
}

func (f *fakeHandle) Close() {
	f.closed = true
}

// stubHandle makes newNetlinkHandle return (h, err) for the rest of the test
// and returns the families of every call it received.
func stubHandle(t *testing.T, h netlinkHandle, err error) *[][]int {
	t.Helper()
	calls := &[][]int{}
	orig := newNetlinkHandle
	newNetlinkHandle = func(nlFamilies ...int) (netlinkHandle, error) {
		*calls = append(*calls, append([]int{}, nlFamilies...))
		if err != nil {
			return nil, err
		}
		return h, nil
	}
	t.Cleanup(func() { newNetlinkHandle = orig })
	return calls
}

// testLink returns a dummy link with the given index, name and flags.
func testLink(index int, name string, flags net.Flags) netlink.Link {
	return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Index: index, Name: name, Flags: flags}}
}

// testAddr returns the address cidr as a netlink address.
func testAddr(t *testing.T, cidr string) netlink.Addr {
	t.Helper()
	ip, ipNet, err := net.ParseCIDR(cidr)
	require.NoError(t, err)
	ipNet.IP = ip
	return netlink.Addr{IPNet: ipNet}
}

// testDst returns cidr as a route destination.
func testDst(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, dst, err := net.ParseCIDR(cidr)
	require.NoError(t, err)
	return dst
}

// eth0Handle returns a fake handle with a loopback and an eth0 that has an
// address and a default route.
func eth0Handle(t *testing.T) *fakeHandle {
	t.Helper()
	return &fakeHandle{
		links: []netlink.Link{
			testLink(1, "lo", net.FlagLoopback),
			testLink(2, "eth0", net.FlagUp),
		},
		addrs: map[int][]netlink.Addr{
			1: {testAddr(t, "127.0.0.1/8")},
			2: {testAddr(t, "10.4.1.2/24")},
		},
		routes: map[int][]netlink.Route{
			2: {{LinkIndex: 2, Dst: nil}},
		},
	}
}

func TestDiscoveryHandleAsksForRouteOnly(t *testing.T) {
	route := [][]int{{unix.NETLINK_ROUTE}}

	t.Run("discoverContainerIface", func(t *testing.T) {
		h := eth0Handle(t)
		calls := stubHandle(t, h, nil)

		link, err := discoverContainerIface()

		require.NoError(t, err)
		assert.Equal(t, "eth0", link.Attrs().Name)
		assert.Equal(t, route, *calls)
		assert.True(t, h.closed)
	})

	t.Run("CleanupAllUruncTaps", func(t *testing.T) {
		h := eth0Handle(t)
		calls := stubHandle(t, h, nil)

		require.NoError(t, CleanupAllUruncTaps())
		assert.Equal(t, route, *calls)
		assert.True(t, h.closed)
	})
}

func TestDiscoverContainerIfaceSentinel(t *testing.T) {
	injected := errors.New("injected")
	eth := testLink(2, "eth0", net.FlagUp)
	addr := map[int][]netlink.Addr{2: {testAddr(t, "10.4.1.2/24")}}
	subnet := map[int][]netlink.Route{2: {{LinkIndex: 2, Dst: testDst(t, "10.4.1.0/24")}}}

	tests := []struct {
		name      string
		handle    *fakeHandle
		handleErr error
		want      string
		sentinel  bool
	}{
		{
			name:     "no links",
			handle:   &fakeHandle{},
			sentinel: true,
		},
		{
			name: "loopback only",
			handle: &fakeHandle{
				links:  []netlink.Link{testLink(1, "lo", net.FlagLoopback)},
				addrs:  map[int][]netlink.Addr{1: {testAddr(t, "127.0.0.1/8")}},
				routes: map[int][]netlink.Route{1: {{LinkIndex: 1}}},
			},
			sentinel: true,
		},
		{
			name: "address but no default route",
			handle: &fakeHandle{
				links:  []netlink.Link{eth},
				addrs:  addr,
				routes: subnet,
			},
			sentinel: true,
		},
		{
			name: "default route but no address",
			handle: &fakeHandle{
				links:  []netlink.Link{eth},
				routes: map[int][]netlink.Route{2: {{LinkIndex: 2}}},
			},
			sentinel: true,
		},
		{
			name: "default route with nil Dst",
			handle: &fakeHandle{
				links:  []netlink.Link{eth},
				addrs:  addr,
				routes: map[int][]netlink.Route{2: {{LinkIndex: 2}}},
			},
			want: "eth0",
		},
		{
			name: "default route 0.0.0.0/0",
			handle: &fakeHandle{
				links:  []netlink.Link{eth},
				addrs:  addr,
				routes: map[int][]netlink.Route{2: {{LinkIndex: 2, Dst: testDst(t, "0.0.0.0/0")}}},
			},
			want: "eth0",
		},
		{
			name: "default route ::/0",
			handle: &fakeHandle{
				links:  []netlink.Link{eth},
				addrs:  addr,
				routes: map[int][]netlink.Route{2: {{LinkIndex: 2, Dst: testDst(t, "::/0")}}},
			},
			want: "eth0",
		},
		{
			name:      "handle constructor error",
			handleErr: injected,
		},
		{
			name:   "LinkList error",
			handle: &fakeHandle{listErr: injected},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var h netlinkHandle
			if tt.handle != nil {
				h = tt.handle
			}
			stubHandle(t, h, tt.handleErr)

			link, err := discoverContainerIface()

			switch {
			case tt.want != "":
				require.NoError(t, err)
				assert.Equal(t, tt.want, link.Attrs().Name)
			case tt.sentinel:
				require.ErrorIs(t, err, ErrNoContainerIface)
				assert.Nil(t, link)
			default:
				require.Error(t, err)
				assert.NotErrorIs(t, err, ErrNoContainerIface)
				require.ErrorIs(t, err, injected)
				assert.Nil(t, link)
			}
			if tt.handle != nil && tt.handleErr == nil {
				assert.True(t, tt.handle.closed, "discovery must close the handle it opened")
			}
		})
	}
}

// No link matches ^tap\d+_urunc$, so cleanup makes no kernel call.
func TestCleanupAllUruncTapsNoTaps(t *testing.T) {
	t.Run("eth0 only", func(t *testing.T) {
		h := eth0Handle(t)
		stubHandle(t, h, nil)
		require.NoError(t, CleanupAllUruncTaps())
		assert.True(t, h.closed)
	})

	t.Run("no links", func(t *testing.T) {
		h := &fakeHandle{}
		stubHandle(t, h, nil)
		require.NoError(t, CleanupAllUruncTaps())
		assert.True(t, h.closed)
	})
}

func TestSelectContainerIfaceListErrors(t *testing.T) {
	t.Run("addr error is not the sentinel", func(t *testing.T) {
		h := eth0Handle(t)
		h.addrErr = unix.EPERM
		_, err := selectContainerIface(h, h.links)
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrNoContainerIface)
		assert.ErrorIs(t, err, unix.EPERM)
	})
	t.Run("route error is not the sentinel", func(t *testing.T) {
		h := eth0Handle(t)
		h.routeErr = unix.ENOBUFS
		_, err := selectContainerIface(h, h.links)
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrNoContainerIface)
	})
	t.Run("interrupted dump keeps its results", func(t *testing.T) {
		h := eth0Handle(t)
		h.addrErr = netlink.ErrDumpInterrupted
		h.routeErr = netlink.ErrDumpInterrupted
		link, err := selectContainerIface(h, h.links)
		require.NoError(t, err)
		assert.Equal(t, "eth0", link.Attrs().Name)
	})
}

func stubCleanupSeams(t *testing.T, calls *[]string) {
	t.Helper()
	of, oq, ot, od := deleteAllTCFiltersFn, deleteAllQDiscsFn, deleteTapDeviceFn, deleteIngressQdiscFn
	deleteAllTCFiltersFn = func(d, e netlink.Link) error {
		*calls = append(*calls, "filters:"+d.Attrs().Name+":"+ethName(e))
		return nil
	}
	deleteAllQDiscsFn = func(d, e netlink.Link) error {
		*calls = append(*calls, "qdiscs:"+d.Attrs().Name+":"+ethName(e))
		return nil
	}
	deleteTapDeviceFn = func(l netlink.Link) error {
		*calls = append(*calls, "tap:"+l.Attrs().Name)
		return nil
	}
	deleteIngressQdiscFn = func(l netlink.Link) error {
		*calls = append(*calls, "ingress:"+l.Attrs().Name)
		return nil
	}
	t.Cleanup(func() {
		deleteAllTCFiltersFn, deleteAllQDiscsFn, deleteTapDeviceFn, deleteIngressQdiscFn = of, oq, ot, od
	})
}

func ethName(l netlink.Link) string {
	if l == nil {
		return "<nil>"
	}
	return l.Attrs().Name
}

func TestCleanupAllUruncTapsWithoutContainerIface(t *testing.T) {
	var calls []string
	stubCleanupSeams(t, &calls)
	h := &fakeHandle{links: []netlink.Link{
		testLink(1, "lo", net.FlagLoopback),
		testLink(3, "tap0_urunc", net.FlagUp),
		testLink(4, "tap1_urunc", net.FlagUp),
		testLink(5, "other_urunc", net.FlagUp),
	}}
	stubHandle(t, h, nil)

	require.NoError(t, CleanupAllUruncTaps())
	assert.Equal(t, []string{
		"filters:tap0_urunc:<nil>", "qdiscs:tap0_urunc:<nil>", "tap:tap0_urunc",
		"filters:tap1_urunc:<nil>", "qdiscs:tap1_urunc:<nil>", "tap:tap1_urunc",
	}, calls)
}

func TestCleanupAllUruncTapsWithContainerIface(t *testing.T) {
	var calls []string
	stubCleanupSeams(t, &calls)
	h := eth0Handle(t)
	h.links = append(h.links, testLink(3, "tap0_urunc", net.FlagUp))
	stubHandle(t, h, nil)

	require.NoError(t, CleanupAllUruncTaps())
	assert.Equal(t, []string{
		"filters:tap0_urunc:eth0", "qdiscs:tap0_urunc:eth0", "tap:tap0_urunc",
	}, calls)
}

func TestRollbackTap(t *testing.T) {
	tap := testLink(3, "tap0_urunc", net.FlagUp)
	eth := testLink(2, "eth0", net.FlagUp)

	t.Run("eth qdisc added by this setup is removed", func(t *testing.T) {
		var calls []string
		stubCleanupSeams(t, &calls)
		rollbackTap(tap, eth, true)
		assert.Equal(t, []string{"ingress:eth0", "tap:tap0_urunc"}, calls)
	})
	t.Run("eth qdisc not added by this setup is kept", func(t *testing.T) {
		var calls []string
		stubCleanupSeams(t, &calls)
		rollbackTap(tap, eth, false)
		assert.Equal(t, []string{"tap:tap0_urunc"}, calls)
	})
}

func TestNewNetworkManager(t *testing.T) {
	tests := []struct {
		name        string
		networkType string
		expectedErr bool
	}{
		{
			name:        "static network manager",
			networkType: "static",
			expectedErr: false,
		},
		{
			name:        "dynamic network manager",
			networkType: "dynamic",
			expectedErr: false,
		},
		{
			name:        "invalid network type",
			networkType: "invalid",
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewNetworkManager(tt.networkType)
			if tt.expectedErr {
				assert.Error(t, err, "NewNetworkManager() should return an error")
			} else {
				assert.NoError(t, err, "NewNetworkManager() should not return an error")
				assert.NotNil(t, got, "NewNetworkManager() should return a non-nil manager")
			}
		})
	}
}
