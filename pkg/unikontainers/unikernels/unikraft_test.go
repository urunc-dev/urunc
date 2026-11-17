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
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

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

func TestUnikraftNetdevIPPrefixLength(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		version         string
		mask            string
		expectedAddress string
		expectedMask    string
	}{
		{
			name:            "a /16 subnet gives a /16 prefix",
			version:         "0.18.0",
			mask:            "255.255.0.0",
			expectedAddress: "netdev.ip=172.17.0.2/16:172.17.0.1:8.8.8.8",
		},
		{
			name:            "a /24 subnet gives a /24 prefix",
			version:         "0.18.0",
			mask:            "255.255.255.0",
			expectedAddress: "netdev.ip=172.17.0.2/24:172.17.0.1:8.8.8.8",
		},
		{
			name:            "a /20 subnet gives a /20 prefix",
			version:         "0.18.0",
			mask:            "255.255.240.0",
			expectedAddress: "netdev.ip=172.17.0.2/20:172.17.0.1:8.8.8.8",
		},
		{
			name:            "an undefined version uses the mask too",
			version:         "",
			mask:            "255.255.0.0",
			expectedAddress: "netdev.ip=172.17.0.2/16:172.17.0.1:8.8.8.8",
		},
		{
			name:            "without a mask the prefix stays /24",
			version:         "0.18.0",
			mask:            "",
			expectedAddress: "netdev.ip=172.17.0.2/24:172.17.0.1:8.8.8.8",
		},
		{
			name:            "versions before 0.16.1 still get the mask as is",
			version:         "0.15.0",
			mask:            "255.255.0.0",
			expectedAddress: "netdev.ipv4_addr=172.17.0.2",
			expectedMask:    "netdev.ipv4_subnet_mask=255.255.0.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := &Unikraft{Version: tc.version}
			err := u.configureUnikraftArgs("initrd", "172.17.0.2", "172.17.0.1", tc.mask, "")
			if tc.version == "" {
				assert.ErrorIs(t, err, ErrUndefinedVersion)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expectedAddress, u.Net.Address)
			assert.Equal(t, tc.expectedMask, u.Net.Mask)
		})
	}
}

func TestUnikraftNetdevIPInvalidMask(t *testing.T) {
	t.Parallel()

	u := &Unikraft{Version: "0.18.0"}
	err := u.configureUnikraftArgs("initrd", "172.17.0.2", "172.17.0.1", "255.255.0", "")
	assert.ErrorContains(t, err, "failed to convert subnet mask")
}
