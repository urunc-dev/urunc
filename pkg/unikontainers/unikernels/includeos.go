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

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const IncludeosUnikernel string = "includeos"

type IncludeOS struct {
	Command string
	Monitor string
	Net     IncludeOSNet
	Block   []IncludeOSBlock
}

type IncludeOSNet struct {
	Address string
	Gateway string
	Mask    string
}

type IncludeOSBlock struct {
	ID       string
	HostPath string
}

func (i *IncludeOS) CommandString() (string, error) {
	var parts []string
	if i.Net.Address != "" {
		parts = append(parts, i.Net.Address, i.Net.Gateway, i.Net.Mask)
	}
	if i.Command != "" {
		parts = append(parts, i.Command)
	}
	return strings.Join(parts, " "), nil
}

func (i *IncludeOS) SupportsBlock() bool {
	return true
}

// SupportsFS returns whether IncludeOS supports shared filesystem mounts (such as 9pfs or virtiofs).
// IncludeOS does not support 9pfs or virtiofs; block storage is handled via SupportsBlock.
func (i *IncludeOS) SupportsFS(_ string) bool {
	return false
}

func (i *IncludeOS) MonitorNetCli(ifName string, mac string) string {
	switch i.Monitor {
	case "hvt", "spt":
		netOption := "--net:service=" + ifName
		netOption += " --net-mac:service=" + mac
		return netOption
	case "qemu":
		return ""
	default:
		return ""
	}
}

func (i *IncludeOS) MonitorBlockCli() []types.MonitorBlockArgs {
	if len(i.Block) == 0 {
		return nil
	}
	switch i.Monitor {
	case "hvt", "spt":
		return []types.MonitorBlockArgs{
			{
				ID:   "rootfs",
				Path: i.Block[0].HostPath,
			},
		}
	case "qemu":
		return []types.MonitorBlockArgs{
			{
				ID:   "rootfs",
				Path: i.Block[0].HostPath,
			},
		}
	default:
		return nil
	}
}

func (i *IncludeOS) MonitorCli() types.MonitorCliArgs {
	return types.MonitorCliArgs{}
}

func (i *IncludeOS) Init(data types.UnikernelParams) error {
	if data.Net.Mask != "" {
		i.Net.Address = data.Net.IP
		i.Net.Gateway = data.Net.Gateway
		i.Net.Mask = data.Net.Mask
	}

	i.Block = make([]IncludeOSBlock, 0, len(data.Block))
	for _, blk := range data.Block {
		newBlk := IncludeOSBlock{
			ID:       blk.ID,
			HostPath: blk.Source,
		}
		i.Block = append(i.Block, newBlk)
	}

	i.Command = strings.Join(data.CmdLine, " ")
	i.Monitor = data.Monitor

	return nil
}

func newIncludeos() *IncludeOS {
	includeosStruct := new(IncludeOS)
	return includeosStruct
}
