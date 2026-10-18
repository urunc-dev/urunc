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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestUnikraftNew(t *testing.T) {
	t.Parallel()

	u, err := New(UnikraftUnikernel)
	require.NoError(t, err)
	assert.IsType(t, &Unikraft{}, u)
}

func TestUnikraftNetArgs(t *testing.T) {
	t.Parallel()

	withNet := types.NetDevParams{IP: "10.0.0.2", Gateway: "10.0.0.1", Mask: "255.255.255.0"}

	testCases := []struct {
		name        string
		version     string
		net         types.NetDevParams
		wantNet     UnikraftNet
		wantInitErr error
	}{
		{
			name:    "current with address",
			version: "0.17.0",
			net:     withNet,
			wantNet: UnikraftNet{Address: "netdev.ip=10.0.0.2/24:10.0.0.1:8.8.8.8"},
		},
		{
			name:    "current with address and nameserver",
			version: "0.17.0",
			net:     types.NetDevParams{IP: "10.0.0.2", Gateway: "10.0.0.1", DNSServer: "1.1.1.1"},
			wantNet: UnikraftNet{Address: "netdev.ip=10.0.0.2/24:10.0.0.1:1.1.1.1"},
		},
		{
			name:    "current without address",
			version: "0.17.0",
		},
		{
			name:        "undefined version without address",
			wantInitErr: ErrUndefinedVersion,
		},
		{
			name:    "compat with address",
			version: "0.15.0",
			net:     withNet,
			wantNet: UnikraftNet{
				Address: "netdev.ipv4_addr=10.0.0.2",
				Gateway: "netdev.ipv4_gw_addr=10.0.0.1",
				Mask:    "netdev.ipv4_subnet_mask=255.255.255.0",
			},
		},
		{
			name:    "compat without address",
			version: "0.15.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u := newUnikraft()
			err := u.Init(types.UnikernelParams{
				CmdLine: []string{"/app"},
				Monitor: "qemu",
				Version: tc.version,
				Net:     tc.net,
				Rootfs:  types.RootfsParams{Type: "initrd"},
			})
			if tc.wantInitErr != nil {
				assert.ErrorIs(t, err, tc.wantInitErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantNet, u.Net)

			cmd, err := u.CommandString()
			require.NoError(t, err)
			if tc.net.IP == "" {
				assert.NotContains(t, cmd, "netdev.")
			} else {
				assert.Contains(t, cmd, tc.wantNet.Address)
			}
			assert.True(t, strings.HasSuffix(cmd, "-- /app"), "command must end with the app cmdline: %q", cmd)
		})
	}
}

func TestUnikraftMonitorCli(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		unikraft *Unikraft
		expected types.MonitorCliArgs
	}{
		{
			name:     "qemu takes everything from the kernel command line",
			unikraft: &Unikraft{Monitor: "qemu", Command: "/app --flag"},
			expected: types.MonitorCliArgs{},
		},
		{
			name:     "firecracker takes everything from the kernel command line",
			unikraft: &Unikraft{Monitor: "firecracker", Command: "/app --flag"},
			expected: types.MonitorCliArgs{},
		},
		{
			name:     "hyperlight passes the command as --guest-exec",
			unikraft: &Unikraft{Monitor: "hyperlight-unikraft", Command: "/entrypoint.py --fast"},
			expected: types.MonitorCliArgs{OtherArgs: []string{"--guest-exec=/entrypoint.py --fast"}},
		},
		{
			name:     "hyperlight keeps a leading dash inside the command value",
			unikraft: &Unikraft{Monitor: "hyperlight-unikraft", Command: "--net"},
			expected: types.MonitorCliArgs{OtherArgs: []string{"--guest-exec=--net"}},
		},
		{
			name:     "hyperlight without a command leaves the entrypoint to hluk",
			unikraft: &Unikraft{Monitor: "hyperlight-unikraft"},
			expected: types.MonitorCliArgs{},
		},
		{
			name: "hyperlight forwards the environment as --env",
			unikraft: &Unikraft{
				Monitor: "hyperlight-unikraft",
				Command: "/entrypoint.py",
				Env:     []string{"FOO=bar", "PATH=/usr/bin:/bin", "EMPTY="},
			},
			expected: types.MonitorCliArgs{OtherArgs: []string{
				"--guest-exec=/entrypoint.py",
				"--env=FOO=bar",
				"--env=PATH=/usr/bin:/bin",
				"--env=EMPTY=",
			}},
		},
		{
			name:     "hyperlight forwards the environment without a command",
			unikraft: &Unikraft{Monitor: "hyperlight-unikraft", Env: []string{"FOO=bar"}},
			expected: types.MonitorCliArgs{OtherArgs: []string{"--env=FOO=bar"}},
		},
		{
			name:     "qemu never forwards the environment as --env",
			unikraft: &Unikraft{Monitor: "qemu", Env: []string{"FOO=bar"}},
			expected: types.MonitorCliArgs{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, tc.unikraft.MonitorCli())
		})
	}
}
