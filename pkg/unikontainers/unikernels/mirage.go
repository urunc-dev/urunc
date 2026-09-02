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

const MirageUnikernel string = "mirage"

type Mirage struct {
	Command     string
	Monitor     string
	Net         MirageNet
	Block       []MirageBlock
	netDevName  string
	blkDevNames []string
}

type MirageNet struct {
	Address string
	Gateway string
}

type MirageBlock struct {
	ID       string
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
		netOption := "--net:" + m.netDevName + "=" + ifName
		netOption += " --net-mac:" + m.netDevName + "=" + mac
		return netOption
	default:
		return ""
	}
}

func (m *Mirage) MonitorBlockCli() []types.MonitorBlockArgs {
	if len(m.Block) == 0 {
		return nil
	}
	switch m.Monitor {
	case "hvt", "spt":
		// Solo5 attaches each block device using an ID that the guest
		// also knows from its manifest. Image builders declare those IDs
		// at build time through the solo5BlkDev annotation as an ordered
		// comma separated list, which we map positionally onto the block
		// devices here. When no ID is given for a device, we fall back to
		// the historical default of "storage".
		args := make([]types.MonitorBlockArgs, 0, len(m.Block))
		for i, blk := range m.Block {
			id := "storage"
			if i < len(m.blkDevNames) {
				id = m.blkDevNames[i]
			}
			args = append(args, types.MonitorBlockArgs{
				ID:   id,
				Path: blk.HostPath,
			})
		}
		return args
	default:
		return nil
	}
}

func (m *Mirage) MonitorCli() types.MonitorCliArgs {
	return types.MonitorCliArgs{}
}

func (m *Mirage) Init(data types.UnikernelParams) error {
	// if Mask is empty, there is no network support
	if data.Net.Mask != "" {
		mask, err := subnetMaskToCIDR(data.Net.Mask)
		if err != nil {
			return err
		}
		// Mirage groups a network device's command line options under a prefix,
		// and that prefix is the device name. The default device "service" uses
		// the plain --ipv4 and --ipv4-gateway flags, but any other device needs
		// its name as the prefix, like --management-ipv4 for a device called
		// "management". So we only add the prefix when the device is not "service".
		if data.NetDevName != "" && data.NetDevName != "service" {
			m.Net.Address = fmt.Sprintf("--%s-ipv4=%s/%d", data.NetDevName, data.Net.IP, mask)
			m.Net.Gateway = "--" + data.NetDevName + "-ipv4-gateway=" + data.Net.Gateway
		} else {
			m.Net.Address = fmt.Sprintf("--ipv4=%s/%d", data.Net.IP, mask)
			m.Net.Gateway = "--ipv4-gateway=" + data.Net.Gateway
		}
	}
	m.Block = make([]MirageBlock, 0, len(data.Block))
	for _, blk := range data.Block {
		newBlk := MirageBlock{
			ID:       blk.ID,
			HostPath: blk.Source,
		}
		m.Block = append(m.Block, newBlk)
	}

	m.Command = strings.Join(data.CmdLine, " ")
	m.Monitor = data.Monitor

	if data.NetDevName != "" {
		m.netDevName = data.NetDevName
	} else {
		m.netDevName = "service"
	}
	if data.BlkDevName != "" {
		names := strings.Split(data.BlkDevName, ",")
		for i := range names {
			names[i] = strings.TrimSpace(names[i])
		}
		m.blkDevNames = names
	}

	return nil
}

func newMirage() *Mirage {
	mirageStruct := new(Mirage)
	return mirageStruct
}
