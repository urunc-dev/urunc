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
	"errors"
)

type Unikernel interface {
	Init(UnikernelParams) error
	CommandString() (string, error)
	SupportsBlock() bool
	SupportsFS(string) bool
	MonitorNetCli(string, string, string) string
	MonitorBlockCli(string) string
	MonitorCli(string) string
}

// UnikernelParams holds the data required to build the unikernels commandline
type UnikernelParams struct {
	CmdLine          []string // The cmdline provided by the image
	EnvVars          []string // The environment variables provided by the image
	EthDeviceIP      string   // The eth device IP
	EthDeviceMask    string   // The eth device mask
	EthDeviceGateway string   // The eth device gateway
	RootFSType       string   // The rootfs type of the Unikernel
	BlockMntPoint    string   // The mount point for the block device
	Version          string   // The version of the unikernel
}

var ErrNotSupportedUnikernel = errors.New("unikernel is not supported")

func New(unikernelType string) (Unikernel, error) {
	switch unikernelType {
	case RumprunUnikernel:
		unikernel := newRumprun()
		return unikernel, nil
	case UnikraftUnikernel:
		unikernel := newUnikraft()
		return unikernel, nil
	case MirageUnikernel:
		unikernel := newMirage()
		return unikernel, nil
	case MewzUnikernel:
		unikernel := newMewz()
		return unikernel, nil
	case LinuxUnikernel:
		unikernel := newLinux()
		return unikernel, nil
	default:
		return nil, ErrNotSupportedUnikernel
	}
}
