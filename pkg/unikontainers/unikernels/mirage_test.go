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

func TestMirageMonitorNetCli(t *testing.T) {
	for _, mon := range []string{"hvt", "spt"} {
		m := &Mirage{Monitor: mon}
		cli := m.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff")
		assert.Contains(t, cli, "--net:service=tap0")
		assert.Contains(t, cli, "--net-mac:service=aa:bb:cc:dd:ee:ff")
	}

	m := &Mirage{Monitor: "qemu"}
	assert.Empty(t, m.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff"))

	m.Monitor = "firecracker"
	assert.Empty(t, m.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff"))
}

func TestMirageMonitorBlockCli(t *testing.T) {
	m := &Mirage{Monitor: "hvt"}
	assert.Nil(t, m.MonitorBlockCli(), "empty block list should return nil")

	for _, mon := range []string{"hvt", "spt"} {
		m = &Mirage{
			Monitor: mon,
			Block: []MirageBlock{
				{ID: "b0", HostPath: "/dev/vda"},
				{ID: "b1", HostPath: "/dev/vdb"},
			},
		}
		got := m.MonitorBlockCli()
		if assert.Len(t, got, 1) {
			assert.Equal(t, "storage", got[0].ID)
			assert.Equal(t, "/dev/vda", got[0].Path)
		}
	}

	m = &Mirage{
		Monitor: "qemu",
		Block:   []MirageBlock{{ID: "b0", HostPath: "/dev/vda"}},
	}
	assert.Nil(t, m.MonitorBlockCli())
}

func TestMirageCommandString(t *testing.T) {
	m := &Mirage{
		Net:     MirageNet{Address: "--ipv4=10.0.0.1/24", Gateway: "--ipv4-gateway=10.0.0.254"},
		Command: "server --port=8080",
	}
	out, err := m.CommandString()
	assert.NoError(t, err)
	assert.Contains(t, out, "--ipv4=10.0.0.1/24")
	assert.Contains(t, out, "--ipv4-gateway=10.0.0.254")
	assert.Contains(t, out, "server --port=8080")
}

func TestMirageInit(t *testing.T) {
	t.Run("with network", func(t *testing.T) {
		m := newMirage()
		err := m.Init(types.UnikernelParams{
			Monitor: "hvt",
			CmdLine: []string{"--port=80"},
			Net: types.NetDevParams{
				IP:      "192.168.1.5",
				Gateway: "192.168.1.1",
				Mask:    "255.255.255.0",
			},
		})
		assert.NoError(t, err)
		assert.Equal(t, "hvt", m.Monitor)
		assert.Equal(t, "--ipv4=192.168.1.5/24", m.Net.Address)
		assert.Equal(t, "--ipv4-gateway=192.168.1.1", m.Net.Gateway)
		assert.Equal(t, "--port=80", m.Command)
	})

	t.Run("non-/24 mask", func(t *testing.T) {
		m := newMirage()
		err := m.Init(types.UnikernelParams{
			Net: types.NetDevParams{
				IP:      "10.0.0.1",
				Mask:    "255.255.255.240",
				Gateway: "10.0.0.14",
			},
		})
		assert.NoError(t, err)
		assert.Equal(t, "--ipv4=10.0.0.1/28", m.Net.Address)
		assert.Equal(t, "--ipv4-gateway=10.0.0.14", m.Net.Gateway)
	})

	t.Run("no network", func(t *testing.T) {
		m := newMirage()
		err := m.Init(types.UnikernelParams{
			Monitor: "spt",
			CmdLine: []string{"arg"},
		})
		assert.NoError(t, err)
		assert.Empty(t, m.Net.Address)
		assert.Empty(t, m.Net.Gateway)
	})

	t.Run("with block devices", func(t *testing.T) {
		m := newMirage()
		err := m.Init(types.UnikernelParams{
			Monitor: "hvt",
			Block: []types.BlockDevParams{
				{ID: "disk0", Source: "/dev/sda", MountPoint: "/mnt"},
				{ID: "disk1", Source: "/dev/sdb", MountPoint: "/data"},
			},
		})
		assert.NoError(t, err)
		if assert.Len(t, m.Block, 2) {
			assert.Equal(t, "disk0", m.Block[0].ID)
			assert.Equal(t, "/dev/sda", m.Block[0].HostPath)
			assert.Equal(t, "disk1", m.Block[1].ID)
		}
	})
}
