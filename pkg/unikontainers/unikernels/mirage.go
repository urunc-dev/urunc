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

const MirageUnikernel string = "mirage"

type Mirage struct {
	Command string
	Monitor string
	Net     MirageNet
	Block   MirageBlock
}

type MirageNet struct {
	Address string
	Gateway string
}

type MirageBlock struct {
	HostPath string
}

func (m *Mirage) CommandString() (string, error) {
	return fmt.Sprintf("%s %s %s", m.Net.Address,
		m.Net.Gateway,
		m.Command), nil
}

func (m *Mirage) SupportsBlock() bool {
	return true
}

func (m *Mirage) SupportsFS(_ string) bool {
	return false
}

func (m *Mirage) MonitorNetCli(ifName string, mac string) string {
	switch m.Monitor {
	case "hvt", "spt":
		netOption := "--net:service=" + ifName
		netOption += " --net-mac:service=" + mac
		return netOption
	default:
		return ""
	}
}

func (m *Mirage) MonitorBlockCli() types.MonitorBlockArgs {
	switch m.Monitor {
	case "hvt", "spt":
		return types.MonitorBlockArgs{
			ID:   "storage",
			Path: m.Block.HostPath,
		}
	default:
		return types.MonitorBlockArgs{}
	}
}

func (m *Mirage) MonitorCli() types.MonitorCliArgs {
	return types.MonitorCliArgs{}
}

func (m *Mirage) Init(data types.UnikernelParams) error {
	// if Mask is empty, there is no network support
	if data.Net.Mask != "" {
		m.Net.Address = "--ipv4=" + data.Net.IP + "/24"
		m.Net.Gateway = "--ipv4-gateway=" + data.Net.Gateway
	}
	if data.Block.MountPoint != "" {
		m.Block.HostPath = data.Block.Image
	}

	m.Command = strings.Join(data.CmdLine, " ")
	m.Monitor = data.Monitor

	return nil
}

func newMirage() *Mirage {
	mirageStruct := new(Mirage)
	return mirageStruct
}
