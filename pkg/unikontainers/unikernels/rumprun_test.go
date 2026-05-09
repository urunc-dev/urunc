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

func TestRumprunSupports(t *testing.T) {
	r := newRumprun()
	assert.True(t, r.SupportsBlock())
	assert.True(t, r.SupportsFS("ext2"))
	assert.False(t, r.SupportsFS("ext4"))
	assert.False(t, r.SupportsFS("9pfs"))
	assert.False(t, r.SupportsFS(""))
	assert.Equal(t, types.MonitorCliArgs{}, r.MonitorCli())
}

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
		if assert.Len(t, got, 1) {
			assert.Equal(t, "rootfs", got[0].ID)
			assert.Equal(t, "/dev/vda", got[0].Path)
		}
	}

	r := &Rumprun{Monitor: "qemu", Blk: RumprunBlk{HostPath: "/dev/vda"}}
	assert.Nil(t, r.MonitorBlockCli())
}

func TestRumprunCommandString_CmdOnly(t *testing.T) {
	r := &Rumprun{Command: "/bin/app"}
	out, err := r.CommandString()
	assert.NoError(t, err)
	assert.Contains(t, out, `"cmdline":"/bin/app"`)
	assert.NotContains(t, out, `"net"`)
	assert.NotContains(t, out, `"blk"`)
}

func TestRumprunCommandString_WithNet(t *testing.T) {
	r := &Rumprun{
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
	}
	out, err := r.CommandString()
	assert.NoError(t, err)
	assert.Contains(t, out, `"net"`)
	assert.Contains(t, out, `"addr":"10.0.0.2"`)
	assert.Contains(t, out, `"gw":"10.0.0.1"`)
}

func TestRumprunCommandString_WithBlock(t *testing.T) {
	r := &Rumprun{
		Command: "app",
		Blk: RumprunBlk{
			Source:     "etfs",
			Path:       "/dev/ld0a",
			FsType:     "blk",
			Mountpoint: "/mnt",
		},
	}
	out, err := r.CommandString()
	assert.NoError(t, err)
	assert.Contains(t, out, `"blk"`)
	assert.Contains(t, out, `"source":"etfs"`)
	assert.Contains(t, out, `"mountpoint":"/mnt"`)
}

func TestRumprunCommandString_WithEnvVars(t *testing.T) {
	r := &Rumprun{
		Command: "app",
		Envs:    []string{"FOO=bar", "BAZ=qux"},
	}
	out, err := r.CommandString()
	assert.NoError(t, err)
	assert.Contains(t, out, `"env":"FOO=bar"`)
	assert.Contains(t, out, `"env":"BAZ=qux"`)
}

func TestRumprunInit_WithNetwork(t *testing.T) {
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
}

func TestRumprunInit_NoNetwork(t *testing.T) {
	r := newRumprun()
	err := r.Init(types.UnikernelParams{
		Monitor: "hvt",
		CmdLine: []string{"app"},
	})
	assert.NoError(t, err)
	assert.Empty(t, r.Net.Address)
}

func TestRumprunInit_WithBlock(t *testing.T) {
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
}

func TestRumprunInit_NoBlock(t *testing.T) {
	r := newRumprun()
	err := r.Init(types.UnikernelParams{Monitor: "hvt", CmdLine: []string{"app"}})
	assert.NoError(t, err)
	assert.Empty(t, r.Blk.Source)
}
