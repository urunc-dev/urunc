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

package unikontainers

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/network"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// fakeNetManager is a network.Manager that returns fixed results.
type fakeNetManager struct {
	info *network.UnikernelNetworkInfo
	err  error
}

func (f *fakeNetManager) NetworkSetup(_ uint32, _ uint32) (*network.UnikernelNetworkInfo, error) {
	return f.info, f.err
}

// stubNetworkManager makes newNetworkManager return (m, err) for the rest of
// the test. Tests that use it must not call t.Parallel.
func stubNetworkManager(t *testing.T, m network.Manager, err error) {
	t.Helper()
	orig := newNetworkManager
	newNetworkManager = func(string) (network.Manager, error) {
		return m, err
	}
	t.Cleanup(func() { newNetworkManager = orig })
}

func TestStubNetworkManagerIsUsed(t *testing.T) {
	stubNetworkManager(t, &fakeNetManager{info: &network.UnikernelNetworkInfo{
		TapDevice: "tap0_urunc",
		EthDevice: network.Interface{IP: "10.4.1.2"},
	}}, nil)

	params, err := SetupNet("dynamic", 0, 0)

	require.NoError(t, err)
	assert.Equal(t, "10.4.1.2", params.IP)
}

func TestSetupNetNoContainerIface(t *testing.T) {
	stubNetworkManager(t, &fakeNetManager{err: fmt.Errorf("wrapped: %w", network.ErrNoContainerIface)}, nil)

	params, err := SetupNet("dynamic", 0, 0)

	require.NoError(t, err)
	assert.Equal(t, types.NetDevParams{}, params)
	assert.Equal(t, "", params.IP)
}

func TestSetupNetOtherErrorFails(t *testing.T) {
	cause := errors.New("protocol not supported")
	stubNetworkManager(t, &fakeNetManager{err: fmt.Errorf("failed to open a netlink route handle: %w", cause)}, nil)

	params, err := SetupNet("dynamic", 0, 0)

	require.Error(t, err)
	assert.ErrorIs(t, err, cause)
	assert.Contains(t, err.Error(), "network setup (dynamic) failed")
	assert.Equal(t, types.NetDevParams{}, params)
}

func TestSetupNetSuccess(t *testing.T) {
	stubNetworkManager(t, &fakeNetManager{info: &network.UnikernelNetworkInfo{
		TapDevice: "tap0_urunc",
		EthDevice: network.Interface{
			IP:             "10.4.1.2",
			Mask:           "255.255.255.0",
			DefaultGateway: "10.4.1.1",
			MAC:            "aa:bb:cc:dd:ee:ff",
			MTU:            1450,
		},
	}}, nil)

	params, err := SetupNet("static", 0, 0)

	require.NoError(t, err)
	assert.Equal(t, "tap0_urunc", params.TapDev)
	assert.Equal(t, "10.4.1.2", params.IP)
	assert.Equal(t, "255.255.255.0", params.Mask)
	assert.Equal(t, "10.4.1.1", params.Gateway)
	assert.Equal(t, "aa:bb:cc:dd:ee:ff", params.MAC)
	assert.EqualValues(t, 1450, params.MTU)
}

func TestSetupNetManagerError(t *testing.T) {
	stubNetworkManager(t, nil, errors.New("unknown network type"))

	_, err := SetupNet("bogus", 0, 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}
