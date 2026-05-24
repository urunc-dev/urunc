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
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestHermitMonitorNetCli(t *testing.T) {
	for _, mon := range []string{"spt", "hvt", "firecracker", ""} {
		h := &Hermit{Monitor: mon}
		assert.Empty(t, h.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff"),
			"monitor=%q should produce no netCli", mon)
	}

	h := &Hermit{Monitor: "qemu"}

	cli := h.MonitorNetCli("tap0", "")
	assert.Contains(t, cli, "-netdev tap,id=net0,ifname=tap0,script=no,downscript=no")
	assert.NotContains(t, cli, "mac=")

	cli = h.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff")
	assert.Contains(t, cli, "mac=aa:bb:cc:dd:ee:ff")

	if runtime.GOARCH == "arm64" {
		assert.Contains(t, cli, "virtio-net-device")
		assert.NotContains(t, cli, "virtio-net-pci")
	} else {
		assert.Contains(t, cli, "virtio-net-pci")
		assert.Contains(t, cli, "disable-legacy=on")
	}
}

func TestHermitCommandString(t *testing.T) {
	cases := []struct {
		name     string
		in       Hermit
		expected string
	}{
		{
			name:     "empty",
			in:       Hermit{},
			expected: "",
		},
		{
			name:     "command only",
			in:       Hermit{Command: "echo hi"},
			expected: "echo hi",
		},
		{
			name:     "ip only inserts no separator",
			in:       Hermit{Net: HermitNet{Address: "10.0.0.2", Mask: 24}},
			expected: "ip=10.0.0.2/24",
		},
		{
			name:     "ip + gateway",
			in:       Hermit{Net: HermitNet{Address: "10.0.0.2", Mask: 24, Gateway: "10.0.0.1"}},
			expected: "ip=10.0.0.2/24 gateway=10.0.0.1",
		},
		{
			name: "ip + cmd inserts -- separator",
			in: Hermit{
				Command: "/usr/bin/app --flag",
				Net:     HermitNet{Address: "10.0.0.2", Mask: 24},
			},
			expected: "ip=10.0.0.2/24 -- /usr/bin/app --flag",
		},
		{
			name: "whitespace-only command treated as empty",
			in: Hermit{
				Command: "   ",
				Net:     HermitNet{Address: "10.0.0.2", Mask: 24},
			},
			expected: "ip=10.0.0.2/24",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.CommandString()
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestHermitInit(t *testing.T) {
	tests := []struct {
		name   string
		params types.UnikernelParams
		check  func(t *testing.T, h *Hermit, err error)
	}{
		{
			name: "no network",
			params: types.UnikernelParams{
				CmdLine: []string{"/bin/run", "arg1", "arg2"},
				Monitor: "qemu",
			},
			check: func(t *testing.T, h *Hermit, err error) {
				assert.NoError(t, err)
				assert.Equal(t, "/bin/run arg1 arg2", h.Command)
				assert.Equal(t, "qemu", h.Monitor)
				assert.Equal(t, "", h.Net.Address)
				assert.Equal(t, 0, h.Net.Mask)
				cmdStr, _ := h.CommandString()
				assert.Equal(t, "/bin/run arg1 arg2", cmdStr)
			},
		},
		{
			name: "with mask computes CIDR",
			params: types.UnikernelParams{
				CmdLine: []string{"app"},
				Monitor: "qemu",
				Net: types.NetDevParams{
					IP:      "10.0.0.5",
					Mask:    "255.255.255.0",
					Gateway: "10.0.0.1",
				},
			},
			check: func(t *testing.T, h *Hermit, err error) {
				assert.NoError(t, err)
				assert.Equal(t, "10.0.0.5", h.Net.Address)
				assert.Equal(t, 24, h.Net.Mask)
				assert.Equal(t, "10.0.0.1", h.Net.Gateway)
				cs, _ := h.CommandString()
				assert.Equal(t, "ip=10.0.0.5/24 gateway=10.0.0.1 -- app", cs)
			},
		},
		{
			name: "bad mask returns error",
			params: types.UnikernelParams{
				Monitor: "qemu",
				Net: types.NetDevParams{
					IP:   "10.0.0.5",
					Mask: "not-a-mask",
				},
			},
			check: func(t *testing.T, h *Hermit, err error) {
				assert.ErrorContains(t, err, "invalid")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHermit()
			err := h.Init(tc.params)
			tc.check(t, h, err)
		})
	}
}
