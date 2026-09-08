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

package unikernels

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestIncludeOSCommandString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		includeos *IncludeOS
		expected  string
	}{
		{
			name:      "empty configuration",
			includeos: &IncludeOS{},
			expected:  "",
		},
		{
			name: "with network and command",
			includeos: &IncludeOS{
				Net: IncludeOSNet{
					Address: "192.168.1.10",
					Gateway: "192.168.1.1",
					Mask:    "255.255.255.0",
				},
				Command: "my-app --arg1",
			},
			expected: "192.168.1.10 192.168.1.1 255.255.255.0 my-app --arg1",
		},
		{
			name: "with network only",
			includeos: &IncludeOS{
				Net: IncludeOSNet{
					Address: "10.0.0.2",
					Gateway: "10.0.0.1",
					Mask:    "255.0.0.0",
				},
			},
			expected: "10.0.0.2 10.0.0.1 255.0.0.0",
		},
		{
			name: "with command only",
			includeos: &IncludeOS{
				Command: "run server",
			},
			expected: "run server",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := tc.includeos.CommandString()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestIncludeOSInit(t *testing.T) {
	t.Parallel()

	params := types.UnikernelParams{
		CmdLine: []string{"hello", "world"},
		Monitor: "qemu",
		Net: types.NetDevParams{
			IP:      "192.168.1.20",
			Gateway: "192.168.1.1",
			Mask:    "255.255.255.0",
		},
		Block: []types.BlockDevParams{
			{
				ID:     "disk0",
				Source: "/dev/loop0",
			},
		},
	}

	inc := newIncludeos()
	err := inc.Init(params)
	require.NoError(t, err)

	assert.Equal(t, "hello world", inc.Command)
	assert.Equal(t, "qemu", inc.Monitor)
	assert.Equal(t, "192.168.1.20", inc.Net.Address)
	assert.Equal(t, "192.168.1.1", inc.Net.Gateway)
	assert.Equal(t, "255.255.255.0", inc.Net.Mask)
	require.Len(t, inc.Block, 1)
	assert.Equal(t, "disk0", inc.Block[0].ID)
	assert.Equal(t, "/dev/loop0", inc.Block[0].HostPath)

	// Test Init with empty network mask
	emptyNetParams := types.UnikernelParams{
		CmdLine: []string{"test"},
		Monitor: "hvt",
		Net: types.NetDevParams{
			IP:   "10.0.0.1",
			Mask: "",
		},
	}
	inc2 := newIncludeos()
	err = inc2.Init(emptyNetParams)
	require.NoError(t, err)
	assert.Empty(t, inc2.Net.Address)
	assert.Empty(t, inc2.Net.Mask)
}

func TestIncludeOSSupports(t *testing.T) {
	t.Parallel()

	inc := newIncludeos()
	assert.True(t, inc.SupportsBlock())
	assert.False(t, inc.SupportsFS("ext4"))
	assert.False(t, inc.SupportsFS("9pfs"))
	assert.False(t, inc.SupportsFS("virtiofs"))
}

func TestIncludeOSMonitorNetCli(t *testing.T) {
	t.Parallel()

	tests := []struct {
		monitor  string
		expected string
	}{
		{
			monitor:  "hvt",
			expected: "--net:service=tap0_urunc --net-mac:service=52:54:00:12:34:56",
		},
		{
			monitor:  "spt",
			expected: "--net:service=tap0_urunc --net-mac:service=52:54:00:12:34:56",
		},
		{
			monitor:  "qemu",
			expected: "",
		},
		{
			monitor:  "unknown",
			expected: "",
		},
	}

	for _, tt := range tests {
		inc := &IncludeOS{Monitor: tt.monitor}
		res := inc.MonitorNetCli("tap0_urunc", "52:54:00:12:34:56")
		assert.Equal(t, tt.expected, res)
	}
}

func TestIncludeOSMonitorBlockCli(t *testing.T) {
	t.Parallel()

	// Empty block devices
	incEmpty := &IncludeOS{Monitor: "qemu"}
	assert.Nil(t, incEmpty.MonitorBlockCli())

	// Configured block devices
	inc := &IncludeOS{
		Block: []IncludeOSBlock{
			{
				ID:       "disk0",
				HostPath: "/tmp/rootfs.raw",
			},
		},
	}

	for _, mon := range []string{"hvt", "spt", "qemu"} {
		inc.Monitor = mon
		blockCli := inc.MonitorBlockCli()
		require.Len(t, blockCli, 1)
		assert.Equal(t, "rootfs", blockCli[0].ID)
		assert.Equal(t, "/tmp/rootfs.raw", blockCli[0].Path)
	}

	inc.Monitor = "other"
	assert.Nil(t, inc.MonitorBlockCli())
}

func TestIncludeOSNewFactory(t *testing.T) {
	t.Parallel()

	uk, err := New(IncludeosUnikernel)
	require.NoError(t, err)
	assert.NotNil(t, uk)
	_, ok := uk.(*IncludeOS)
	assert.True(t, ok)
}
