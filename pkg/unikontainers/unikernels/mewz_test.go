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
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestMewzReportsNoBlockOrFS(t *testing.T) {
	m := newMewz()
	assert.False(t, m.SupportsBlock())
	for _, fs := range []string{"ext4", "9pfs", "virtiofs", "initrd", ""} {
		assert.False(t, m.SupportsFS(fs))
	}
	assert.Nil(t, m.MonitorBlockCli())
}

func TestMewzCommandStringFormat(t *testing.T) {
	m := &Mewz{
		Net: MewzNet{Address: "10.0.0.2", Mask: 24, Gateway: "10.0.0.1"},
	}
	got, err := m.CommandString()
	assert.NoError(t, err)
	assert.Equal(t, "ip=10.0.0.2/24 gateway=10.0.0.1 ", got)
}

func TestMewzMonitorNetCli(t *testing.T) {
	m := &Mewz{Monitor: "qemu"}
	cli := m.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff")
	assert.Contains(t, cli, "virtio-net-pci")
	assert.Contains(t, cli, "mac=aa:bb:cc:dd:ee:ff")
	assert.Contains(t, cli, "ifname=tap0")
	assert.Contains(t, cli, "id=net0")

	for _, mon := range []string{"hvt", "spt", "firecracker", ""} {
		assert.Empty(t, (&Mewz{Monitor: mon}).MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff"),
			"monitor %q should not emit net cli", mon)
	}
}

func TestMewzMonitorCli(t *testing.T) {
	got := (&Mewz{Monitor: "qemu"}).MonitorCli()
	assert.Contains(t, got.OtherArgs, "-no-reboot")
	assert.Contains(t, got.OtherArgs, "isa-debug-exit")
	assert.Empty(t, got.ExtraInitrd)

	assert.Equal(t, types.MonitorCliArgs{}, (&Mewz{Monitor: "spt"}).MonitorCli())
}

func TestMewzInit(t *testing.T) {
	t.Run("explicit mask is converted", func(t *testing.T) {
		m := newMewz()
		err := m.Init(types.UnikernelParams{
			CmdLine: []string{"server", "--port=80"},
			Monitor: "qemu",
			Net: types.NetDevParams{
				IP:      "192.168.1.10",
				Mask:    "255.255.0.0",
				Gateway: "192.168.1.1",
			},
		})
		assert.NoError(t, err)
		assert.Equal(t, "192.168.1.10", m.Net.Address)
		assert.Equal(t, 16, m.Net.Mask)
		assert.Equal(t, "192.168.1.1", m.Net.Gateway)
		assert.Equal(t, "server --port=80", m.Command)
		assert.Equal(t, "qemu", m.Monitor)
	})

	t.Run("missing mask defaults to /24", func(t *testing.T) {
		m := newMewz()
		err := m.Init(types.UnikernelParams{
			CmdLine: []string{"server"},
			Monitor: "qemu",
			Net:     types.NetDevParams{IP: "10.0.0.2", Gateway: "10.0.0.1"},
		})
		assert.NoError(t, err)
		assert.Equal(t, 24, m.Net.Mask)

		out, _ := m.CommandString()
		assert.True(t, strings.HasPrefix(out, "ip=10.0.0.2/24"))
	})

	t.Run("garbage mask returns error", func(t *testing.T) {
		err := newMewz().Init(types.UnikernelParams{
			Monitor: "qemu",
			Net:     types.NetDevParams{IP: "10.0.0.2", Mask: "definitely.not.a.mask"},
		})
		assert.Error(t, err)
	})
}
