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
)

// setupStubs drives networkSetup without a kernel. The fail fields name the
// step that is refused; calls records the steps in order, rollback included.
type setupStubs struct {
	failUp     string // link name whose LinkSetUp fails
	failQdisc  string // link name whose ingress qdisc add fails
	failFilter bool
	failAddr   bool
	calls      []string
}

func (s *setupStubs) install(t *testing.T) {
	t.Helper()
	od, ot := deleteIngressQdiscFn, deleteTapDeviceFn
	oc, ou, oq, oa, orf := createTapDeviceFn, linkSetUpFn, addIngressQdiscFn, addrReplaceFn, redirectFilterAdd
	deleteTapDeviceFn = func(l netlink.Link) error {
		s.calls = append(s.calls, "del-tap:"+l.Attrs().Name)
		return nil
	}
	deleteIngressQdiscFn = func(l netlink.Link) error {
		s.calls = append(s.calls, "del-ingress:"+l.Attrs().Name)
		return nil
	}
	createTapDeviceFn = func(name string, _ int, _, _ uint32) (netlink.Link, error) {
		s.calls = append(s.calls, "create:"+name)
		return testLink(9, name, net.FlagUp), nil
	}
	linkSetUpFn = func(l netlink.Link) error {
		s.calls = append(s.calls, "up:"+l.Attrs().Name)
		if l.Attrs().Name == s.failUp {
			return errors.New("up refused")
		}
		return nil
	}
	addIngressQdiscFn = func(l netlink.Link) error {
		s.calls = append(s.calls, "qdisc:"+l.Attrs().Name)
		if l.Attrs().Name == s.failQdisc {
			return errors.New("qdisc refused")
		}
		return nil
	}
	redirectFilterAdd = func(kind string, source, _ netlink.Link) error {
		s.calls = append(s.calls, "filter:"+kind+":"+source.Attrs().Name)
		if s.failFilter {
			return errors.New("filter refused")
		}
		return nil
	}
	addrReplaceFn = func(l netlink.Link, _ *netlink.Addr) error {
		s.calls = append(s.calls, "addr:"+l.Attrs().Name)
		if s.failAddr {
			return errors.New("addr refused")
		}
		return nil
	}
	t.Cleanup(func() {
		deleteIngressQdiscFn, deleteTapDeviceFn = od, ot
		createTapDeviceFn, linkSetUpFn, addIngressQdiscFn, addrReplaceFn, redirectFilterAdd = oc, ou, oq, oa, orf
	})
}

func TestNetworkSetupRollsBackEveryFailurePoint(t *testing.T) {
	eth := testLink(2, "eth0", net.FlagUp)
	tests := []struct {
		name     string
		stubs    setupStubs
		addTC    bool
		ip       string
		wantErr  string
		wantTail []string
	}{
		{
			name:     "tap link up",
			stubs:    setupStubs{failUp: "tap0_urunc"},
			addTC:    true,
			wantErr:  "LinkSetUp(tap0_urunc)",
			wantTail: []string{"del-tap:tap0_urunc"},
		},
		{
			name:     "eth link up",
			stubs:    setupStubs{failUp: "eth0"},
			addTC:    true,
			wantErr:  "LinkSetUp(eth0)",
			wantTail: []string{"del-tap:tap0_urunc"},
		},
		{
			name:     "tap qdisc",
			stubs:    setupStubs{failQdisc: "tap0_urunc"},
			addTC:    true,
			wantErr:  "addIngressQdisc(tap=tap0_urunc)",
			wantTail: []string{"del-tap:tap0_urunc"},
		},
		{
			// The eth qdisc add failed, so this setup added nothing there and
			// a CNI-owned ingress qdisc must stay.
			name:     "eth qdisc keeps the CNI qdisc",
			stubs:    setupStubs{failQdisc: "eth0"},
			addTC:    true,
			wantErr:  "addIngressQdisc(redirect=eth0)",
			wantTail: []string{"del-tap:tap0_urunc"},
		},
		{
			name:     "filter removes the qdisc this setup added",
			stubs:    setupStubs{failFilter: true},
			addTC:    true,
			wantErr:  "addRedirectFilter(tap0_urunc->eth0)",
			wantTail: []string{"del-ingress:eth0", "del-tap:tap0_urunc"},
		},
		{
			name:     "address",
			stubs:    setupStubs{failAddr: true},
			ip:       "172.16.1.1/24",
			wantErr:  "AddrReplace(tap0_urunc",
			wantTail: []string{"del-tap:tap0_urunc"},
		},
		{
			name:     "unparsable address",
			ip:       "not-an-ip",
			wantErr:  "ParseAddr(not-an-ip)",
			wantTail: []string{"del-tap:tap0_urunc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.stubs
			s.install(t)

			link, err := networkSetup("tap0_urunc", tt.ip, eth, tt.addTC, 1000, 1000)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Nil(t, link)
			require.GreaterOrEqual(t, len(s.calls), len(tt.wantTail))
			assert.Equal(t, tt.wantTail, s.calls[len(s.calls)-len(tt.wantTail):])
		})
	}
}

func TestNetworkSetupSuccessKeepsTap(t *testing.T) {
	s := &setupStubs{}
	s.install(t)

	link, err := networkSetup("tap0_urunc", "172.16.1.1/24", testLink(2, "eth0", net.FlagUp), true, 1000, 1000)

	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, "tap0_urunc", link.Attrs().Name)
	for _, c := range s.calls {
		assert.NotContains(t, c, "del-")
	}
}

func TestStaticNetworkSetupRemovesTapWhenNATFails(t *testing.T) {
	s := &setupStubs{}
	s.install(t)
	stubHandle(t, eth0Handle(t), nil)
	orig := setNATRuleFn
	setNATRuleFn = func(string, string) error { return errors.New("iptables refused") }
	t.Cleanup(func() { setNATRuleFn = orig })

	info, err := StaticNetwork{}.NetworkSetup(1000, 1000)

	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "iptables refused")
	assert.Equal(t, "del-tap:tap0_urunc", s.calls[len(s.calls)-1])
	assert.NotContains(t, s.calls, "del-ingress:eth0")
}

func TestDynamicNetworkSetupRemovesTapAndQdiscWhenInfoFails(t *testing.T) {
	s := &setupStubs{}
	s.install(t)
	stubHandle(t, eth0Handle(t), nil)
	origIfaces := netInterfacesFn
	netInterfacesFn = func() ([]net.Interface, error) { return []net.Interface{{Name: "lo"}, {Name: "eth0"}}, nil }
	t.Cleanup(func() { netInterfacesFn = origIfaces })
	orig := getInterfaceInfoFn
	getInterfaceInfoFn = func(string) (Interface, error) { return Interface{}, errors.New("no gateway") }
	t.Cleanup(func() { getInterfaceInfoFn = orig })

	info, err := DynamicNetwork{}.NetworkSetup(1000, 1000)

	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "no gateway")
	assert.Equal(t, []string{"del-ingress:eth0", "del-tap:tap0_urunc"}, s.calls[len(s.calls)-2:])
}
