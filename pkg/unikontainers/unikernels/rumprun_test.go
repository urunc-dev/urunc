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

func TestRumprunMonitorNetCli(t *testing.T) {
	for _, mon := range []string{"hvt", "spt"} {
		r := &Rumprun{Monitor: mon}
		cli := r.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff")
		assert.Contains(t, cli, "--net:tap=tap0")
		assert.Contains(t, cli, "--net-mac:tap=aa:bb:cc:dd:ee:ff")
	}

	r := &Rumprun{Monitor: "qemu"}
	assert.Empty(t, r.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff"))
}

func TestRumprunMonitorBlockCli(t *testing.T) {
	for _, mon := range []string{"hvt", "spt"} {
		r := &Rumprun{
			Monitor: mon,
			Blk:     RumprunBlk{HostPath: "/dev/vda"},
		}
		got := r.MonitorBlockCli()
		require.Len(t, got, 1)
		assert.Equal(t, "rootfs", got[0].ID)
		assert.Equal(t, "/dev/vda", got[0].Path)
	}

	r := &Rumprun{Monitor: "qemu", Blk: RumprunBlk{HostPath: "/dev/vda"}}
	assert.Nil(t, r.MonitorBlockCli())
}

func TestRumprunCommandString(t *testing.T) {
	tests := []struct {
		name        string
		r           Rumprun
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "cmd only",
			r:           Rumprun{Command: "/bin/app"},
			wantContain: []string{`"cmdline":"/bin/app"`},
			wantAbsent:  []string{`"net"`, `"blk"`},
		},
		{
			name: "with net",
			r: Rumprun{
				Command: "app",
				Net: RumprunNet{
					Interface: "ukvmif0",
					Cloner:    "True",
					Type:      "inet",
					Method:    "static",
					Address:   "10.0.0.2",
					Mask:      "1",
					Gateway:   "10.0.0.1",
				},
			},
			wantContain: []string{`"net"`, `"addr":"10.0.0.2"`, `"gw":"10.0.0.1"`},
		},
		{
			name: "with block",
			r: Rumprun{
				Command: "app",
				Blk: RumprunBlk{
					Source:     "etfs",
					Path:       "/dev/ld0a",
					FsType:     "blk",
					Mountpoint: "/mnt",
				},
			},
			wantContain: []string{`"blk"`, `"source":"etfs"`, `"mountpoint":"/mnt"`},
		},
		{
			name:        "with env vars",
			r:           Rumprun{Command: "app", Envs: []string{"FOO=bar", "BAZ=qux"}},
			wantContain: []string{`"env":"FOO=bar"`, `"env":"BAZ=qux"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.r.CommandString()
			assert.NoError(t, err)
			for _, want := range tc.wantContain {
				assert.Contains(t, out, want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, out, absent)
			}
		})
	}
}

func TestRumprunInit(t *testing.T) {
	t.Run("with network", func(t *testing.T) {
		r := newRumprun()
		err := r.Init(types.UnikernelParams{
			Monitor: "hvt",
			CmdLine: []string{"app"},
			Net: types.NetDevParams{
				IP:      "10.0.0.2",
				Gateway: "10.0.0.1",
				Mask:    "255.255.255.0",
			},
		})
		assert.NoError(t, err)
		assert.Equal(t, "10.0.0.2", r.Net.Address)
		assert.Equal(t, "10.0.0.1", r.Net.Gateway)
		assert.Equal(t, "ukvmif0", r.Net.Interface)
		assert.Equal(t, "static", r.Net.Method)
	})

	t.Run("no network", func(t *testing.T) {
		r := newRumprun()
		err := r.Init(types.UnikernelParams{
			Monitor: "hvt",
			CmdLine: []string{"app"},
		})
		assert.NoError(t, err)
		assert.Empty(t, r.Net.Address)
	})

	t.Run("with block", func(t *testing.T) {
		r := newRumprun()
		err := r.Init(types.UnikernelParams{
			Monitor: "spt",
			CmdLine: []string{"app"},
			Block: []types.BlockDevParams{
				{Source: "/dev/vda", MountPoint: "/mnt"},
			},
		})
		assert.NoError(t, err)
		assert.Equal(t, "etfs", r.Blk.Source)
		assert.Equal(t, "/dev/ld0a", r.Blk.Path)
		assert.Equal(t, "/mnt", r.Blk.Mountpoint)
		assert.Equal(t, "/dev/vda", r.Blk.HostPath)
	})

	t.Run("no block", func(t *testing.T) {
		r := newRumprun()
		err := r.Init(types.UnikernelParams{Monitor: "hvt", CmdLine: []string{"app"}})
		assert.NoError(t, err)
		assert.Empty(t, r.Blk.Source)
	})
}
