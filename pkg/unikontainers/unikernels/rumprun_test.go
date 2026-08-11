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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestRumprunInit(t *testing.T) {
	t.Parallel()
	mask, err := subnetMaskToCIDR(SubnetMask125)
	assert.NoError(t, err)
	expectedMask := fmt.Sprintf("%d", mask)

	testCases := []struct {
		name     string
		params   types.UnikernelParams
		expected *Rumprun
	}{
		{
			name: "Init with network and block",
			params: types.UnikernelParams{
				CmdLine: []string{"app.bin", "arg1"},
				Monitor: "hvt",
				EnvVars: []string{"TEST=true"},
				Net: types.NetDevParams{
					IP:      "10.0.0.2",
					Mask:    "255.255.255.0",
					Gateway: "10.0.0.1",
				},
				Block: []types.BlockDevParams{
					{
						Source:     "/host/path",
						MountPoint: "/mnt",
					},
				},
			},
			expected: &Rumprun{
				Command: "app.bin arg1",
				Monitor: "hvt",
				Envs:    []string{"TEST=true"},
				Net: RumprunNet{
					Interface: "ukvmif0",
					Cloner:    "True",
					Type:      "inet",
					Method:    "static",
					Address:   "10.0.0.2",
					Mask:      expectedMask,
					Gateway:   "10.0.0.1",
				},
				Blk: RumprunBlk{
					Source:     "etfs",
					Path:       "/dev/ld0a",
					FsType:     "blk",
					Mountpoint: "/mnt",
					HostPath:   "/host/path",
				},
			},
		},
		{
			name: "Init without network or block",
			params: types.UnikernelParams{
				CmdLine: []string{"app.bin"},
				Monitor: "spt",
			},
			expected: &Rumprun{
				Command: "app.bin",
				Monitor: "spt",
				Envs:    nil,
				Net: RumprunNet{
					Address: "",
				},
				Blk: RumprunBlk{
					Source: "",
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRumprun()
			err := r.Init(tc.params)
			assert.NoError(t, err)

			assert.Equal(t, tc.expected.Command, r.Command)
			assert.Equal(t, tc.expected.Monitor, r.Monitor)
			assert.Equal(t, tc.expected.Envs, r.Envs)
			assert.Equal(t, tc.expected.Net, r.Net)
			assert.Equal(t, tc.expected.Blk, r.Blk)
		})
	}
}

func TestRumprunCommandString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		rumprun  *Rumprun
		expected string
	}{
		{
			name: "Basic command without network or block",
			rumprun: &Rumprun{
				Command: "nginx -c /nginx.conf",
				Net: RumprunNet{
					Address: "",
				},
				Blk: RumprunBlk{
					Source: "",
				},
			},
			expected: `{"cmdline":"nginx -c /nginx.conf"}`,
		},
		{
			name: "Command with environment variables",
			rumprun: &Rumprun{
				Command: "app",
				Envs:    []string{"ENV1=v1", "ENV2=v2"},
			},
			expected: `{"cmdline":"app","env":"ENV1=v1","env":"ENV2=v2"}`,
		},
		{
			name: "Command with network settings",
			rumprun: &Rumprun{
				Command: "app",
				Net: RumprunNet{
					Interface: "ukvmif0",
					Cloner:    "True",
					Type:      "inet",
					Method:    "static",
					Address:   "10.0.0.2",
					Mask:      "125",
					Gateway:   "10.0.0.1",
				},
			},
			expected: `{"cmdline":"app","net":{"if":"ukvmif0","cloner":"True","type":"inet","method":"static","addr":"10.0.0.2","mask":"125","gw":"10.0.0.1"}}`,
		},
		{
			name: "Command with block storage",
			rumprun: &Rumprun{
				Command: "app",
				Blk: RumprunBlk{
					Source:     "etfs",
					Path:       "/dev/ld0a",
					FsType:     "blk",
					Mountpoint: "/",
				},
			},
			expected: `{"cmdline":"app","blk":{"source":"etfs","path":"/dev/ld0a","fstype":"blk","mountpoint":"/"}}`,
		},
		{
			name: "Command with everything",
			rumprun: &Rumprun{
				Command: "app",
				Envs:    []string{"TEST=true"},
				Net: RumprunNet{
					Interface: "ukvmif0",
					Cloner:    "True",
					Type:      "inet",
					Method:    "static",
					Address:   "10.0.0.2",
					Mask:      "125",
					Gateway:   "10.0.0.1",
				},
				Blk: RumprunBlk{
					Source:     "etfs",
					Path:       "/dev/ld0a",
					FsType:     "blk",
					Mountpoint: "/",
				},
			},
			expected: `{"cmdline":"app","env":"TEST=true","net":{"if":"ukvmif0","cloner":"True","type":"inet","method":"static","addr":"10.0.0.2","mask":"125","gw":"10.0.0.1"},"blk":{"source":"etfs","path":"/dev/ld0a","fstype":"blk","mountpoint":"/"}}`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := tc.rumprun.CommandString()
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestRumprunSupportsBlockFS(t *testing.T) {
	t.Parallel()
	r := newRumprun()
	assert.True(t, r.SupportsBlock())
	assert.True(t, r.SupportsFS("ext2"))
	assert.False(t, r.SupportsFS("9pfs"))
}

func TestRumprunMonitorCli(t *testing.T) {
	t.Parallel()

	r := newRumprun()
	assert.Equal(t, types.MonitorCliArgs{}, r.MonitorCli())

	testCasesNet := []struct {
		monitor  string
		expected string
	}{
		{"hvt", "--net:tap=tap0 --net-mac:tap=00:11:22:33:44:55"},
		{"spt", "--net:tap=tap0 --net-mac:tap=00:11:22:33:44:55"},
		{"unknown", ""},
	}
	for _, tc := range testCasesNet {
		r.Monitor = tc.monitor
		assert.Equal(t, tc.expected, r.MonitorNetCli("tap0", "00:11:22:33:44:55"))
	}

	r.Blk.HostPath = "/host/path"
	testCasesBlk := []struct {
		monitor  string
		expected []types.MonitorBlockArgs
	}{
		{"hvt", []types.MonitorBlockArgs{{ID: "rootfs", Path: "/host/path"}}},
		{"spt", []types.MonitorBlockArgs{{ID: "rootfs", Path: "/host/path"}}},
		{"unknown", nil},
	}
	for _, tc := range testCasesBlk {
		r.Monitor = tc.monitor
		assert.Equal(t, tc.expected, r.MonitorBlockCli())
	}
}
