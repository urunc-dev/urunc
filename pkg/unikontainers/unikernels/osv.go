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
	Envs    []string
	Net     OSvNet
	Blk     OSvBlk
}

type OSvNet struct {
	Address string
	Mask    string
	Gateway string
}

type OSvBlk struct {
	HostPath string
}

func newOSv() *OSv {
	return new(OSv)
}

func (o *OSv) Init(data types.UnikernelParams) error {
	o.Monitor = data.Monitor
	o.Command = strings.Join(data.CmdLine, " ")
	o.Envs = data.EnvVars

	if data.Net.IP != "" {
		o.Net.Address = data.Net.IP
		o.Net.Mask = data.Net.Mask
		o.Net.Gateway = data.Net.Gateway
	}
	if len(data.Block) > 0 {
		o.Blk.HostPath = data.Block[0].Source
	}

	return nil
}

func (o *OSv) CommandString() (string, error) {
	var parts []string

	if o.Net.Address != "" {
		netArg := fmt.Sprintf("--ip=eth0,%s,%s", o.Net.Address, o.Net.Mask)
		if o.Net.Gateway != "" {
			netArg += fmt.Sprintf(" --defaultgw=%s", o.Net.Gateway)
		}
		parts = append(parts, netArg)
	}

	for _, env := range o.Envs {
		parts = append(parts, fmt.Sprintf("--env=%s", env))
	}

	if o.Command != "" {
		parts = append(parts, o.Command)
	}

	return strings.Join(parts, " "), nil
}

func (o *OSv) SupportsBlock() bool {
	return true
}

func (o *OSv) SupportsFS(fsType string) bool {
	switch fsType {
	case "rofs", "zfs", "block", "raw":
		return true
	default:
		return false
	}
}

func (o *OSv) MonitorNetCli(ifname string, mac string) string {
	return ""
}

func (o *OSv) MonitorBlockCli() []types.MonitorBlockArgs {
	if o.Blk.HostPath == "" {
		return nil
	}
	return []types.MonitorBlockArgs{
		{
			ID:   "usr",
			Path: o.Blk.HostPath,
		},
	}
}

func (o *OSv) MonitorCli() types.MonitorCliArgs {
	return types.MonitorCliArgs{
		OtherArgs: " -no-reboot",
	}
}
