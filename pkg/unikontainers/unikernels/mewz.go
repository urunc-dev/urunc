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

const MewzUnikernel string = "mewz"

type Mewz struct {
	Command string
	Monitor string
	Net     MewzNet
}

type MewzNet struct {
	Address string
	Mask    int
	Gateway string
}

func (m *Mewz) CommandString() (string, error) {
	return fmt.Sprintf("ip=%s/%d gateway=%s ", m.Net.Address, m.Net.Mask,
		m.Net.Gateway), nil
}

func (m *Mewz) SupportsBlock() bool {
	return false
}

func (m *Mewz) SupportsFS(_ string) bool {
	return false
}

func (m *Mewz) MonitorNetCli(ifName string, mac string) string {
	switch m.Monitor {
	case "qemu":
		ncli := " -device virtio-net-pci,netdev=net0,disable-legacy=on,disable-modern=off,mac=" + mac
		ncli += " -netdev tap,script=no,downscript=no,id=net0,ifname=" + ifName
		return ncli
	default:
		return ""
	}
}

// Mewz does not seem to support virtio block or anu other kind of block/fs.
func (m *Mewz) MonitorBlockCli() []types.MonitorBlockArgs {
	return nil
}

// Mewz does not require any monitor specific cli option
func (m *Mewz) MonitorCli() types.MonitorCliArgs {
	switch m.Monitor {
	case "qemu":
		return types.MonitorCliArgs{
			OtherArgs: " -no-reboot -device isa-debug-exit,iobase=0x501,iosize=2",
		}
	default:
		return types.MonitorCliArgs{}
	}
}

func (m *Mewz) Init(data types.UnikernelParams) error {
	var mask int
	if data.Net.Mask != "" {
		var err error
		mask, err = subnetMaskToCIDR(data.Net.Mask)
		if err != nil {
			return err
		}
	} else {
		mask = 24
	}
	m.Command = strings.Join(data.CmdLine, " ")
	m.Monitor = data.Monitor
	m.Net.Address = data.Net.IP
	m.Net.Gateway = data.Net.Gateway
	m.Net.Mask = mask

	return nil
}

func newMewz() *Mewz {
	mewzStruct := new(Mewz)
	return mewzStruct
}
