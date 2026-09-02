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
	VFS     OSvVFS
}

type OSvNet struct {
	Address string
	Mask    string
	Gateway string
}

type OSvVFS struct {
	RootFS string
}

func (o *OSv) CommandString() (string, error) {
	envVarString := ""
	if len(o.Env) > 0 {
		envVarString = "env=" + strings.Join(o.Env, ",")
	}

	return fmt.Sprintf("%s %s %s %s %s -- %s",
		o.Command,
		envVarString,
		o.Net.Address,
		o.Net.Gateway,
		o.Net.Mask,
		o.VFS.RootFS), nil
}

func (o *OSv) SupportsBlock() bool {
	return true 
}

func (o *OSv) SupportsFS(fsType string) bool {
	switch fsType {
	case "block":
		return true
	default:
		return false
	}
}

func (o *OSv) MonitorNetCli(ifName string, mac string) string {
	// OSv with QEMU uses standard virtio-net configuration
	return ""
}

func (o *OSv) MonitorBlockCli() []types.MonitorBlockArgs {
	return nil
}

func (o *OSv) MonitorCli() types.MonitorCliArgs {
	return types.MonitorCliArgs{}
}

func (o *OSv) Init(data types.UnikernelParams) error {
	o.Env = data.EnvVars
	o.Monitor = data.Monitor
	o.Command = strings.Join(data.CmdLine, " ")

	// Configure networking
	if data.Net.IP != "" {
		o.Net.Address = "gw=" + data.Net.Gateway
		o.Net.Mask = "netmask=" + data.Net.Mask
		o.Net.Gateway = "ip=" + data.Net.IP
	}

	// Configure filesystem
	if len(data.Block) > 0 {
		o.VFS.RootFS = "root=/dev/vda"
	}

	return nil
}

func newOSv() *OSv {
	osvStruct := new(OSv)
	return osvStruct
}