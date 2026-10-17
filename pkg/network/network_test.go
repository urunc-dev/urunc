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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

// rollbackTap has no caller until networkSetup's error paths use it; this
// keeps the unused linter quiet until then. Drop it once they do.
var _ = rollbackTap

// fakeHandle is a netlinkHandle backed by maps keyed by link index, so
// discovery can run without a netlink socket.
type fakeHandle struct {
	links   []netlink.Link
	addrs   map[int][]netlink.Addr
	routes  map[int][]netlink.Route
	listErr error
	closed  bool
}

func (f *fakeHandle) LinkList() ([]netlink.Link, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.links, nil
}

func (f *fakeHandle) AddrList(link netlink.Link, _ int) ([]netlink.Addr, error) {
	return f.addrs[link.Attrs().Index], nil
}

func (f *fakeHandle) RouteList(link netlink.Link, _ int) ([]netlink.Route, error) {
	return f.routes[link.Attrs().Index], nil
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

func TestStubHandleIsUsed(t *testing.T) {
	h := &fakeHandle{}
	calls := stubHandle(t, h, nil)

	link, err := discoverContainerIface()

	require.ErrorIs(t, err, ErrNoContainerIface)
	assert.Nil(t, link)
	assert.Len(t, *calls, 1)
	assert.True(t, h.closed, "discovery must close the handle it opened")
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
