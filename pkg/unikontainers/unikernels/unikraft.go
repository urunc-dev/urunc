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
	"fmt"
	"runtime"
	"strings"

	version "github.com/hashicorp/go-version"
)

const UnikraftUnikernel string = "unikraft"
const UnikraftCompatVersion string = "0.16.1"

var ErrUndefinedVersion = errors.New("version is undefined, using default version")
var ErrVersionParsing = errors.New("failed to parse provided version, using default version")

type Unikraft struct {
	AppName string
	Command string
	Env     []string
	Net     UnikraftNet
	VFS     UnikraftVFS
	Version string
}

type UnikraftNet struct {
	Address string
	Mask    string
	Gateway string
}

type UnikraftVFS struct {
	RootFS string
}

func (u *Unikraft) CommandString() (string, error) {
	envVarString := ""
	consoleStr := ""

	if runtime.GOARCH == "arm64" {
		consoleStr = "console=ttyS0"
	}

	if len(u.Env) > 0 {
		envVarString = "env.vars=[ " + strings.Join(u.Env, " ") + " ]"
	}

	return fmt.Sprintf("%s %s %s %s %s %s %s -- %s", u.AppName,
		consoleStr,
		envVarString,
		u.Net.Address,
		u.Net.Gateway,
		u.Net.Mask,
		u.VFS.RootFS,
		u.Command), nil
}

func (u *Unikraft) SupportsBlock() bool {
	return false
}

func (u *Unikraft) SupportsFS(_ string) bool {
	return false
}

// There is no need for any changes here yet.
func (u *Unikraft) MonitorNetCli(_ string) string {
	return ""
}

// We have not managed to make Unikraft run with block yet.
func (u *Unikraft) MonitorBlockCli(_ string) string {
	return ""
}

// There are no generic CLI hypervisor options for Unikraft yet.
func (u *Unikraft) MonitorCli(_ string) string {
	return ""
}

func (u *Unikraft) Init(data UnikernelParams) error {
	u.Env = data.EnvVars
	u.Version = data.Version
	u.AppName = "Unikraft"
	u.Command = strings.Join(data.CmdLine, " ")

	return u.configureUnikraftArgs(data.RootFSType, data.EthDeviceIP, data.EthDeviceGateway, data.EthDeviceMask)
}

func (u *Unikraft) configureUnikraftArgs(rootFsType, ethDeviceIP, ethDeviceGateway, ethDeviceMask string) error {
	setCompatArgs := func() {
		u.Net.Address = "netdev.ipv4_addr=" + ethDeviceIP
		u.Net.Gateway = "netdev.ipv4_gw_addr=" + ethDeviceGateway
		u.Net.Mask = "netdev.ipv4_subnet_mask=" + ethDeviceMask
		// TODO: We need to add support for actual block devices (e.g. virtio-blk)
		// and sharedfs or any other Unikraft related ways to pass data to guest.
		if rootFsType == "initrd" {
			u.VFS.RootFS = "vfs.rootfs=" + "initrd"
		} else {
			u.VFS.RootFS = ""
		}
	}

	setCurrentArgs := func() {
		u.Net.Address = "netdev.ip=" + ethDeviceIP + "/24:" + ethDeviceGateway + ":8.8.8.8"
		// TODO: We need to add support for actual block devices (e.g. virtio-blk)
		// and sharedfs or any other Unikraft related ways to pass data to guest.
		if rootFsType == "initrd" {
			// TODO: This needs better handling. We need to revisit this
			// when we better understand all the available options for
			// passing info inside unikraft unikernels.
			u.VFS.RootFS = "vfs.fstab=[ \"initrd0:/:extract:::\" ]"
		} else {
			u.VFS.RootFS = ""
		}
	}

	if u.Version == "" {
		setCurrentArgs()
		return ErrUndefinedVersion
	}

	unikernelVersion, err := version.NewVersion(u.Version)
	if err != nil {
		setCurrentArgs()
		return ErrVersionParsing
	}

	targetVersion, err := version.NewVersion(UnikraftCompatVersion)
	if err != nil {
		return fmt.Errorf("failed to parse default version: %w", err)
	}

	if unikernelVersion.GreaterThanOrEqual(targetVersion) {
		setCurrentArgs()
	} else {
		setCompatArgs()
		// Remove environment variables, since old versions do not
		// support them
		u.Env = nil
	}
	return nil
}

func newUnikraft() *Unikraft {
	unikraftStruct := new(Unikraft)
	return unikraftStruct
}
