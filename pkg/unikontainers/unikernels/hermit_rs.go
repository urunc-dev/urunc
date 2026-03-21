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
	"runtime"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const HermitUnikernel string = "hermit"

type Hermit struct {
	Command string
	Monitor string
	Net     HermitNet
}

type HermitNet struct {
	Address string
	Mask    int
	Gateway string
}

func (h *Hermit) CommandString() (string, error) {
	kernelArgs := make([]string, 0, 2)

	if h.Net.Address != "" {
		kernelArgs = append(kernelArgs, fmt.Sprintf("ip=%s/%d", h.Net.Address, h.Net.Mask))
	}
	if h.Net.Gateway != "" {
		kernelArgs = append(kernelArgs, fmt.Sprintf("gateway=%s", h.Net.Gateway))
	}

	args := strings.Join(kernelArgs, " ")
	appArgs := strings.TrimSpace(h.Command)

	switch {
	case args != "" && appArgs != "":
		return args + " -- " + appArgs, nil
	case args != "":
		return args, nil
	default:
		return appArgs, nil
	}
}

func (h *Hermit) SupportsBlock() bool {
	return false
}

func (h *Hermit) SupportsFS(fsType string) bool {
	switch fsType {
	case "initrd":
		return true
	case "virtiofs":
		return true
	default:
		return false
	}
}


func (h *Hermit) MonitorNetCli(ifName string, mac string) string {
	switch h.Monitor {
	case "qemu":
		netdev := fmt.Sprintf(" -netdev tap,id=net0,ifname=%s,script=no,downscript=no", ifName)

		device := "virtio-net-pci"
		deviceArgs := " -device " + device + ",netdev=net0"

		// QEMU on x86_64 typically uses virtio-net-pci.
		// On arm64 virtio-net-device is the safer default.
		if runtime.GOARCH == "arm64" {
			device = "virtio-net-device"
			deviceArgs = " -device " + device + ",netdev=net0"
		} else {
			deviceArgs = " -device " + device + ",netdev=net0,disable-legacy=on"
		}

		if mac != "" {
			deviceArgs += ",mac=" + mac
		}

		return netdev + deviceArgs
	default:
		return ""
	}
}




func (h *Hermit) MonitorBlockCli() []types.MonitorBlockArgs {
	return nil
}

func (h *Hermit) MonitorCli() types.MonitorCliArgs {
	switch h.Monitor {
	case "qemu":
		// isa-debug-exit is x86/QEMU friendly.
		// For arm64, keep it minimal.
		if runtime.GOARCH == "arm64" {
			return types.MonitorCliArgs{
				OtherArgs: " -no-reboot",
			}
		}

		return types.MonitorCliArgs{
			OtherArgs: " -no-reboot -device isa-debug-exit,iobase=0xf4,iosize=0x04",
		}
	default:
		return types.MonitorCliArgs{}
	}
}

func (h *Hermit) Init(data types.UnikernelParams) error {
	mask := 24
	if data.Net.Mask != "" {
		cidr, err := subnetMaskToCIDR(data.Net.Mask)
		if err != nil {
			return err
		}
		mask = cidr
	}

	h.Command = strings.Join(data.CmdLine, " ")
	h.Monitor = data.Monitor
	h.Net.Address = data.Net.IP
	h.Net.Gateway = data.Net.Gateway
	h.Net.Mask = mask

	return nil
}

func newHermit() *Hermit {
	return new(Hermit)
}
