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

// 1. Test CommandString with different flag combinations
func TestOSvCommandString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		osv      *OSv
		expected string
	}{
		{
			name:     "empty configuration",
			osv:      &OSv{},
			expected: "",
		},
		{
			name: "application command only",
			osv: &OSv{
				Command: "/hello",
			},
			expected: "/hello",
		},
		{
			name: "command with environment variables",
			osv: &OSv{
				Command: "/hello",
				Envs:    []string{"PORT=8080", "ENV=prod"},
			},
			expected: "--env=PORT=8080 --env=ENV=prod /hello",
		},
		{
			name: "network configuration only",
			osv: &OSv{
				Net: OSvNet{
					Address: "172.17.0.2",
					Mask:    "255.255.0.0",
					Gateway: "172.17.0.1",
				},
			},
			expected: "--ip=eth0,172.17.0.2,255.255.0.0 --defaultgw=172.17.0.1",
		},
		{
			name: "full configuration: network, env, and command",
			osv: &OSv{
				Command: "/hello world",
				Envs:    []string{"KEY=val"},
				Net: OSvNet{
					Address: "172.17.0.2",
					Mask:    "255.255.0.0",
					Gateway: "172.17.0.1",
				},
			},
			expected: "--ip=eth0,172.17.0.2,255.255.0.0 --defaultgw=172.17.0.1 --env=KEY=val /hello world",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := tc.osv.CommandString()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// 2. Test Factory & Init()
func TestOSvInitAndFactory(t *testing.T) {
	t.Parallel()

	// Verify factory creates an OSv instance
	uk, err := New(OSvUnikernel)
	require.NoError(t, err)
	require.NotNil(t, uk)

	// Test Init populates fields properly
	params := types.UnikernelParams{
		CmdLine: []string{"/app", "arg1"},
		EnvVars: []string{"FOO=bar"},
		Monitor: "qemu",
		Net: types.NetDevParams{
			IP:      "192.168.1.10",
			Mask:    "255.255.255.0",
			Gateway: "192.168.1.1",
		},
		Block: []types.BlockDevParams{
			{Source: "/path/to/usr.img"},
		},
	}

	err = uk.Init(params)
	require.NoError(t, err)

	cmdStr, err := uk.CommandString()
	require.NoError(t, err)
	assert.Contains(t, cmdStr, "--ip=eth0,192.168.1.10,255.255.255.0")
	assert.Contains(t, cmdStr, "--env=FOO=bar")
	assert.Contains(t, cmdStr, "/app arg1")
}

// 3. Test Storage & Filesystem capabilities
func TestOSvCapabilities(t *testing.T) {
	t.Parallel()

	osv := newOSv()
	assert.True(t, osv.SupportsBlock())
	assert.True(t, osv.SupportsFS("rofs"))
	assert.True(t, osv.SupportsFS("zfs"))
	assert.False(t, osv.SupportsFS("ext4"))
}
