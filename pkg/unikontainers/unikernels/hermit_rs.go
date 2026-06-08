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
	var args []string

	if h.Net.Address != "" {
		args = append(args, fmt.Sprintf("ip=%s/%d", h.Net.Address, h.Net.Mask))
	}
	if h.Net.Gateway != "" {
		args = append(args, fmt.Sprintf("gateway=%s", h.Net.Gateway))
	}

	// Add separator ONLY if we have net args AND a command
	appArgs := strings.TrimSpace(h.Command)
	if len(args) > 0 && appArgs != "" {
		args = append(args, "--")
	}

	if appArgs != "" {
		args = append(args, appArgs)
	}

	return strings.Join(args, " "), nil
}

func (h *Hermit) SupportsBlock() bool {
	return false
}

func (h *Hermit) SupportsFS(fsType string) bool {
	return fsType == "initrd"
}

func (h *Hermit) MonitorNetCli(ifName string, mac string) string {
	switch h.Monitor {
	case "qemu":
		netdev := fmt.Sprintf(" -netdev tap,id=net0,ifname=%s,script=no,downscript=no", ifName)

		var deviceArgs string

		// QEMU on x86_64 typically uses virtio-net-pci.
		// On arm64 virtio-net-device is the safer default.
		if runtime.GOARCH == "arm64" {
			deviceArgs = " -device " + "virtio-net-device" + ",netdev=net0"
		} else {
			deviceArgs = " -device " + "virtio-net-pci" + ",netdev=net0,disable-legacy=on"
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
	return types.MonitorCliArgs{
		OtherArgs: " -no-reboot",
	}
}

func (h *Hermit) Init(data types.UnikernelParams) error {

	if data.Net.Mask != "" {
		mask, err := subnetMaskToCIDR(data.Net.Mask)
		if err != nil {
			return err
		}
		h.Net.Address = data.Net.IP
		h.Net.Gateway = data.Net.Gateway
		h.Net.Mask = mask
	}

	h.Command = strings.Join(data.CmdLine, " ")
	h.Monitor = data.Monitor

	return nil
}

func newHermit() *Hermit {
	return new(Hermit)
}
