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
	// IncludeOS typically accepts command-line arguments
	// Network configuration is usually done via the unikernel image itself
	// or via command-line arguments depending on the application
	cmdParts := []string{}

	// Add environment variables if any
	for _, env := range i.Envs {
		cmdParts = append(cmdParts, env)
	}

	// Add network configuration if present
	if i.Net.Address != "" {
		cmdParts = append(cmdParts, fmt.Sprintf("--net=%s/%s", i.Net.Address, i.Net.Mask))
		if i.Net.Gateway != "" {
			cmdParts = append(cmdParts, fmt.Sprintf("--gateway=%s", i.Net.Gateway))
		}
	}

	// Add the main command
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
		// QEMU handles networking through its own options
		// This is typically configured in the hypervisor layer
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
		// Solo5 monitors support block devices with specific IDs
		blockArgs := make([]types.MonitorBlockArgs, 0, len(i.Block))
		for _, blk := range i.Block {
			// Use the first block device or a default ID
			id := blk.ID
			if id == "" {
				id = "storage"
			}
			blockArgs = append(blockArgs, types.MonitorBlockArgs{
				ID:   id,
				Path: blk.HostPath,
			})
		}
		// Solo5 typically supports a single block device, so return the first one
		if len(blockArgs) > 0 {
			return blockArgs[:1]
		}
		return blockArgs
	case "qemu":
		// QEMU handles block devices through its own options
		// This is typically configured in the hypervisor layer
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
	// Initialize network configuration if provided
	if data.Net.Mask != "" {
		i.Net.Address = data.Net.IP
		i.Net.Gateway = data.Net.Gateway
		i.Net.Mask = data.Net.Mask
	}

	// Initialize block devices if provided
	i.Block = make([]IncludeOSBlock, 0, len(data.Block))
	for _, blk := range data.Block {
		newBlk := IncludeOSBlock{
			ID:       blk.ID,
			HostPath: blk.Source,
		}
		i.Block = append(i.Block, newBlk)
	}

	// Set command line and environment variables
	i.Command = strings.Join(data.CmdLine, " ")
	i.Monitor = data.Monitor
	i.Envs = data.EnvVars

	return nil
}

func newIncludeOS() *IncludeOS {
	includeosStruct := new(IncludeOS)
	return includeosStruct
}
