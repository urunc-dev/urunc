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

func TestMirageInitSubnetMask(t *testing.T) {
	tests := []struct {
		name        string
		mask        string
		ip          string
		gateway     string
		wantAddress string
		wantGateway string
	}{
		{
			name:        "non-/24 mask is used correctly",
			mask:        "255.255.255.240",
			ip:          "10.0.0.1",
			gateway:     "10.0.0.14",
			wantAddress: "--ipv4=10.0.0.1/28",
			wantGateway: "--ipv4-gateway=10.0.0.14",
		},
		{
			name:        "/24 mask still works",
			mask:        "255.255.255.0",
			ip:          "192.168.1.5",
			gateway:     "192.168.1.1",
			wantAddress: "--ipv4=192.168.1.5/24",
			wantGateway: "--ipv4-gateway=192.168.1.1",
		},
		{
			name:        "no network when mask is empty",
			mask:        "",
			ip:          "",
			gateway:     "",
			wantAddress: "",
			wantGateway: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMirage()
			params := types.UnikernelParams{
				Net: types.NetDevParams{
					IP:      tt.ip,
					Mask:    tt.mask,
					Gateway: tt.gateway,
				},
			}
			err := m.Init(params)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantAddress, m.Net.Address)
			assert.Equal(t, tt.wantGateway, m.Net.Gateway)
		})
	}
}

func TestMirageNetDevName(t *testing.T) {
	t.Run("uses net device name from annotation", func(t *testing.T) {
		t.Parallel()
		m := &Mirage{}
		err := m.Init(types.UnikernelParams{Monitor: "hvt", NetDevName: "management"})
		assert.NoError(t, err)
		cli := m.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff")
		assert.Contains(t, cli, "--net:management=tap0")
		assert.Contains(t, cli, "--net-mac:management=aa:bb:cc:dd:ee:ff")
	})

	t.Run("falls back to service when annotation is absent", func(t *testing.T) {
		t.Parallel()
		m := &Mirage{}
		err := m.Init(types.UnikernelParams{Monitor: "hvt"})
		assert.NoError(t, err)
		cli := m.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff")
		assert.Contains(t, cli, "--net:service=tap0")
		assert.Contains(t, cli, "--net-mac:service=aa:bb:cc:dd:ee:ff")
	})
}

func TestMirageBlkDevName(t *testing.T) {
	t.Run("uses block device name from annotation", func(t *testing.T) {
		t.Parallel()
		m := &Mirage{}
		err := m.Init(types.UnikernelParams{
			Monitor:    "hvt",
			BlkDevName: "database",
			Block:      []types.BlockDevParams{{Source: "/path/to/img"}},
		})
		assert.NoError(t, err)
		args := m.MonitorBlockCli()
		assert.Len(t, args, 1)
		assert.Equal(t, "database", args[0].ID)
		assert.Equal(t, "/path/to/img", args[0].Path)
	})

	t.Run("falls back to storage when annotation is absent", func(t *testing.T) {
		t.Parallel()
		m := &Mirage{}
		err := m.Init(types.UnikernelParams{
			Monitor: "hvt",
			Block:   []types.BlockDevParams{{Source: "/path/to/img"}},
		})
		assert.NoError(t, err)
		args := m.MonitorBlockCli()
		assert.Len(t, args, 1)
		assert.Equal(t, "storage", args[0].ID)
	})
}
