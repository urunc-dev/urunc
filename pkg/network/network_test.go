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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

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

type networkFixture struct {
	addresses map[string][]netlink.Addr
	routes    map[string][]netlink.Route
}

func (f networkFixture) AddrList(link netlink.Link, _ int) ([]netlink.Addr, error) {
	return f.addresses[link.Attrs().Name], nil
}

func (f networkFixture) RouteList(link netlink.Link, _ int) ([]netlink.Route, error) {
	return f.routes[link.Attrs().Name], nil
}

func TestSelectContainerIface(t *testing.T) {
	t.Parallel()

	link := func(name string) netlink.Link {
		return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
			Name: name, MTU: 1500, HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 1},
		}}
	}
	loopback := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "lo", Flags: net.FlagLoopback}}
	internal := link("eth0")
	bridge := link("eth1")
	tap := link("tap0_urunc")
	disconnected := link("eth2")
	fixture := networkFixture{
		addresses: map[string][]netlink.Addr{
			"eth0":       {{IPNet: &net.IPNet{IP: net.IPv4(172, 18, 5, 2), Mask: net.CIDRMask(16, 32)}}},
			"eth1":       {{IPNet: &net.IPNet{IP: net.IPv4(172, 17, 0, 2), Mask: net.CIDRMask(16, 32)}}},
			"eth2":       {{IPNet: &net.IPNet{IP: net.IPv4(172, 19, 0, 2), Mask: net.CIDRMask(16, 32)}}},
			"tap0_urunc": {{IPNet: &net.IPNet{IP: net.IPv4(172, 16, 1, 1), Mask: net.CIDRMask(24, 32)}}},
		},
		routes: map[string][]netlink.Route{
			"eth0": {{Dst: &net.IPNet{IP: net.IPv4(172, 18, 0, 0), Mask: net.CIDRMask(16, 32)}}},
			"eth1": {{Gw: net.IPv4(172, 17, 0, 1)}},
		},
	}

	t.Run("internal network without a gateway", func(t *testing.T) {
		selected, info, err := selectContainerIface(fixture, []netlink.Link{loopback, tap, disconnected, internal})
		require.NoError(t, err)
		assert.Same(t, internal, selected)
		assert.Equal(t, "172.18.5.2", info.IP)
		assert.Equal(t, "255.255.0.0", info.Mask)
		assert.Empty(t, info.DefaultGateway)
	})
	t.Run("interface with a default route takes precedence", func(t *testing.T) {
		selected, info, err := selectContainerIface(fixture, []netlink.Link{internal, bridge})
		require.NoError(t, err)
		assert.Same(t, bridge, selected)
		assert.Equal(t, "172.17.0.1", info.DefaultGateway)
	})
	t.Run("default route with the lowest metric takes precedence", func(t *testing.T) {
		routed := networkFixture{
			addresses: fixture.addresses,
			routes: map[string][]netlink.Route{
				"eth0": {{Gw: net.IPv4(172, 18, 0, 1), Priority: 200}},
				"eth1": {{Gw: net.IPv4(172, 17, 0, 1), Priority: 100}},
			},
		}
		selected, info, err := selectContainerIface(routed, []netlink.Link{internal, bridge})
		require.NoError(t, err)
		assert.Same(t, bridge, selected)
		assert.Equal(t, "172.17.0.1", info.DefaultGateway)
	})
	t.Run("loopback and leftover TAP are not a container network", func(t *testing.T) {
		_, _, err := selectContainerIface(fixture, []netlink.Link{loopback, tap})
		assert.ErrorIs(t, err, ErrNoContainerIface)
	})
}

type redirectCleanupFixture struct {
	filters        map[int][]netlink.Filter
	qdiscs         map[int][]netlink.Qdisc
	deletedFilters []netlink.Filter
	deletedQdiscs  []netlink.Qdisc
}

func (f *redirectCleanupFixture) FilterList(link netlink.Link, parent uint32) ([]netlink.Filter, error) {
	if parent != ingressFilterParent {
		return nil, nil
	}
	return f.filters[link.Attrs().Index], nil
}

func (f *redirectCleanupFixture) FilterDel(filter netlink.Filter) error {
	f.deletedFilters = append(f.deletedFilters, filter)
	return nil
}

func (f *redirectCleanupFixture) QdiscList(link netlink.Link) ([]netlink.Qdisc, error) {
	return f.qdiscs[link.Attrs().Index], nil
}

func (f *redirectCleanupFixture) QdiscDel(qdisc netlink.Qdisc) error {
	f.deletedQdiscs = append(f.deletedQdiscs, qdisc)
	return nil
}

func TestCleanupTapRedirect(t *testing.T) {
	t.Parallel()

	tap := &netlink.Tuntap{LinkAttrs: netlink.LinkAttrs{Name: "tap0_urunc", Index: 3}}
	original := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 4}}
	other := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "eth1", Index: 5}}
	peerIngress := ingressQdisc(original)
	otherIngress := ingressQdisc(other)
	toTap := redirectFilter(original, tap)
	toPeer := redirectFilter(tap, original)
	foreignFilter := &netlink.Flower{}
	fixture := func() *redirectCleanupFixture {
		return &redirectCleanupFixture{
			filters: map[int][]netlink.Filter{tap.Index: {toPeer}, original.Index: {toTap}, other.Index: {foreignFilter}},
			qdiscs:  map[int][]netlink.Qdisc{original.Index: {peerIngress}, other.Index: {otherIngress}},
		}
	}

	t.Run("only the redirect peer loses its ingress qdisc", func(t *testing.T) {
		f := fixture()
		require.NoError(t, cleanupTapRedirect(f, tap, []netlink.Link{tap, other, original}))
		assert.Equal(t, []netlink.Qdisc{peerIngress}, f.deletedQdiscs)
		assert.Empty(t, f.deletedFilters)
	})
	t.Run("other filters on the peer are preserved", func(t *testing.T) {
		f := fixture()
		f.filters[original.Index] = append(f.filters[original.Index], foreignFilter)
		require.NoError(t, cleanupTapRedirect(f, tap, []netlink.Link{tap, other, original}))
		assert.Empty(t, f.deletedQdiscs)
		assert.Equal(t, []netlink.Filter{toTap}, f.deletedFilters)
	})
	t.Run("a missing peer leaves other links alone", func(t *testing.T) {
		f := fixture()
		require.NoError(t, cleanupTapRedirect(f, tap, []netlink.Link{tap, other}))
		assert.Empty(t, f.deletedQdiscs)
		assert.Empty(t, f.deletedFilters)
	})
	t.Run("a static TAP has no peer redirect", func(t *testing.T) {
		f := fixture()
		delete(f.filters, tap.Index)
		require.NoError(t, cleanupTapRedirect(f, tap, []netlink.Link{tap, other, original}))
		assert.Empty(t, f.deletedQdiscs)
		assert.Empty(t, f.deletedFilters)
	})
}

type redirectSetupFixture struct {
	qdiscAddFailureOn  int
	filterAddFailureOn int
	deletedQdiscs      []int
}

func (f *redirectSetupFixture) QdiscAdd(qdisc netlink.Qdisc) error {
	if qdisc.Attrs().LinkIndex == f.qdiscAddFailureOn {
		return unix.EEXIST
	}
	return nil
}

func (f *redirectSetupFixture) FilterAdd(filter netlink.Filter) error {
	if filter.Attrs().LinkIndex == f.filterAddFailureOn {
		return unix.ENOENT
	}
	return nil
}

func (f *redirectSetupFixture) QdiscDel(qdisc netlink.Qdisc) error {
	f.deletedQdiscs = append(f.deletedQdiscs, qdisc.Attrs().LinkIndex)
	return nil
}

func TestSetupTapRedirectRollsBackOwnedPeerQdisc(t *testing.T) {
	t.Parallel()

	tap := &netlink.Tuntap{LinkAttrs: netlink.LinkAttrs{Name: "tap0_urunc", Index: 3}}
	peer := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 4}}
	tests := []struct {
		name           string
		fixture        *redirectSetupFixture
		wantErr        error
		removedIndices []int
	}{
		{"successful setup keeps the peer qdisc", &redirectSetupFixture{}, nil, nil},
		{"existing peer qdisc stays in place", &redirectSetupFixture{qdiscAddFailureOn: peer.Index}, unix.EEXIST, nil},
		{"outbound redirect failure removes the peer qdisc", &redirectSetupFixture{filterAddFailureOn: tap.Index}, unix.ENOENT, []int{peer.Index}},
		{"inbound redirect failure removes the peer qdisc", &redirectSetupFixture{filterAddFailureOn: peer.Index}, unix.ENOENT, []int{peer.Index}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.ErrorIs(t, setupTapRedirect(tt.fixture, tap, peer), tt.wantErr)
			assert.Equal(t, tt.removedIndices, tt.fixture.deletedQdiscs)
		})
	}
}
