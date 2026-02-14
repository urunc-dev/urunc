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

const OSvUnikernel string = "osv"

type OSv struct {
	Command string
	Monitor string
	Env     []string
	Net     OSvNet
	Block   []OSvBlock
}

type OSvNet struct {
	Address string
	Mask    string
	Gateway string
}

type OSvBlock struct {
	ID       string
	HostPath string
}

func (o *OSv) CommandString() (string, error) {
	// OSv accepts command line arguments directly.
	// Network configuration is passed via --ip, --netmask, and --defaultgw.
	// Environment variables are passed with --env.
	cmdString := ""

	if o.Net.Address != "" {
		cmdString += fmt.Sprintf("--ip=%s ", o.Net.Address)
		if o.Net.Mask != "" {
			cmdString += fmt.Sprintf("--netmask=%s ", o.Net.Mask)
		}
		if o.Net.Gateway != "" {
			cmdString += fmt.Sprintf("--defaultgw=%s ", o.Net.Gateway)
		}
	}

	for _, env := range o.Env {
		cmdString += fmt.Sprintf("--env=%s ", env)
	}

	if o.Command != "" {
		cmdString += "-- " + o.Command
	}

	return strings.TrimSpace(cmdString), nil
}

func (o *OSv) SupportsBlock() bool {
	return true
}

func (o *OSv) SupportsFS(fsType string) bool {
	// OSv primarily uses block devices with its built-in ZFS filesystem
	switch fsType {
	case "zfs":
		return true
	default:
		return false
	}
}

// OSv uses standard QEMU virtio-net configuration.
// No special CLI options are needed.
func (o *OSv) MonitorNetCli(_ string, _ string) string {
	return ""
}

func (o *OSv) MonitorBlockCli() []types.MonitorBlockArgs {
	if len(o.Block) == 0 {
		return nil
	}

	switch o.Monitor {
	case "qemu":
		// OSv uses virtio-blk for block devices
		blockArgs := make([]types.MonitorBlockArgs, 0, len(o.Block))
		for _, block := range o.Block {
			blockArgs = append(blockArgs, types.MonitorBlockArgs{
				ID:   block.ID,
				Path: block.HostPath,
			})
		}
		return blockArgs
	default:
		return nil
	}
}

// OSv does not require any monitor specific CLI options.
func (o *OSv) MonitorCli() types.MonitorCliArgs {
	return types.MonitorCliArgs{}
}

func (o *OSv) Init(data types.UnikernelParams) error {
	o.Command = strings.Join(data.CmdLine, " ")
	o.Monitor = data.Monitor
	o.Env = data.EnvVars

	// Configure network if provided
	if data.Net.Mask != "" {
		o.Net.Address = data.Net.IP
		o.Net.Gateway = data.Net.Gateway
		o.Net.Mask = data.Net.Mask
	}

	// Configure block devices if provided
	if len(data.Block) > 0 {
		o.Block = make([]OSvBlock, 0, len(data.Block))
		for _, block := range data.Block {
			o.Block = append(o.Block, OSvBlock{
				ID:       block.ID,
				HostPath: block.Source,
			})
		}
	}

	return nil
}

func newOSv() *OSv {
	return &OSv{}
}
