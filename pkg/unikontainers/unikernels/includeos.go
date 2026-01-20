// Copyright (c) 2023-2025, Nubificus LTD
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
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const IncludeOSUnikernel string = "includeos"

type IncludeOS struct {
	Command string
	Monitor string
	Envs    []string
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
	cmdParts := []string{}

	// IncludeOS expects network config as a JSON string argument.
	// We construct this manually to ensure it matches the specific schema required by the OS.
	if i.Net.Address != "" {
		// Default to iface 0.
		// Constructing JSON: {"net":[{"iface":0,"address":"...","netmask":"...","gateway":"..."}]}
		gwPart := ""
		if i.Net.Gateway != "" {
			gwPart = fmt.Sprintf(`,"gateway":"%s"`, i.Net.Gateway)
		}

		// We assume i.Net.Mask is in dotted-decimal format (e.g 255.255.255.0)
		// If urunc provides CIDR conversion logic would be needed here
		jsonConfig := fmt.Sprintf(
			`{"net":[{"iface":0,"address":"%s","netmask":"%s"%s}]}`,
			i.Net.Address,
			i.Net.Mask,
			gwPart,
		)
		cmdParts = append(cmdParts, jsonConfig)
	}

	if len(i.Envs) > 0 {
		cmdParts = append(cmdParts, i.Envs...)
	}

	if i.Command != "" {
		cmdParts = append(cmdParts, i.Command)
	}

	return strings.Join(cmdParts, " "), nil
}

func (i *IncludeOS) SupportsBlock() bool {
	return true
}

func (i *IncludeOS) SupportsFS(fsType string) bool {
	switch fsType {
	case "ext2", "ext3", "ext4":
		return true
	default:
		return false
	}
}

func (i *IncludeOS) MonitorNetCli(ifName string, mac string) string {
	switch i.Monitor {
	case "hvt", "spt":
		// Solo5 monitor options for networking
		netOption := "--net:service=" + ifName
		netOption += " --net-mac:service=" + mac
		return netOption
	case "qemu":
		// QEMU handles networking through its own options in the hypervisor layer
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
		// Solo5 monitors support block devices with specific IDs.
		// Note: Solo5 typically supports a single block device.
		blockArgs := make([]types.MonitorBlockArgs, 0, len(i.Block))
		for _, blk := range i.Block {
			id := blk.ID
			if id == "" {
				id = "storage"
			}
			blockArgs = append(blockArgs, types.MonitorBlockArgs{
				ID:   id,
				Path: blk.HostPath,
			})
		}
		// Return only the first block device to ensure compatibility with Solo5
		if len(blockArgs) > 0 {
			return blockArgs[:1]
		}
		return blockArgs
	case "qemu":
		// QEMU handles block devices through its own options
		return nil
	default:
		return nil
	}
}

func (i *IncludeOS) MonitorCli() types.MonitorCliArgs {
	// IncludeOS does not require any generic monitor-specific arguments
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
	i.Envs = data.EnvVars

	return nil
}

func newIncludeOS() *IncludeOS {
	return &IncludeOS{}
}
