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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const RumprunUnikernel string = "rumprun"
const SubnetMask125 = "128.0.0.0"

type Rumprun struct {
	Command string
	Monitor string
	Envs    []string
	Net     RumprunNet
	Blk     RumprunBlk
}

type RumprunCmd struct {
	CmdLine string `json:"cmdline"`
}

type RumprunEnv struct {
	Env string `json:"env"`
}

type RumprunNet struct {
	Interface string `json:"if"`
	Cloner    string `json:"cloner"`
	Type      string `json:"type"`
	Method    string `json:"method"`
	Address   string `json:"addr"`
	Mask      string `json:"mask"`
	Gateway   string `json:"gw"`
}

type RumprunBlk struct {
	HostPath   string `json:"-"`
	Source     string `json:"source"`
	Path       string `json:"path"`
	FsType     string `json:"fstype"`
	Mountpoint string `json:"mountpoint"`
}

func (r *Rumprun) CommandString() (string, error) {
	// Rumprun accepts a JSON string to configure the unikernel. However,
	// Rumprun does not use a valid JSON format. Therefore, we manually
	// construct the JSON instead of using Go's json Marshal.
	// For more information check https://github.com/rumpkernel/rumprun/blob/master/doc/config.md
	cmdJSONString := ""
	envJSONString := ""
	netJSONString := ""
	blkJSONString := ""
	cmd := RumprunCmd{
		CmdLine: r.Command,
	}
	cmdJSON, err := json.Marshal(cmd)
	if err != nil {
		return "", fmt.Errorf("Could not Marshal cmdline: %v", err)
	}
	cmdJSONString = string(cmdJSON)
	for i, eVar := range r.Envs {
		eVar := RumprunEnv{
			Env: eVar,
		}
		oneVarJSON, err := json.Marshal(eVar)
		if err != nil {
			return "", fmt.Errorf("Could not Marshal environment variable: %v", err)
		}
		if i != 0 {
			envJSONString += ","
		}
		oneVarJSONString := string(oneVarJSON)
		oneVarJSONString = strings.TrimPrefix(oneVarJSONString, "{")
		oneVarJSONString = strings.TrimSuffix(oneVarJSONString, "}")
		envJSONString += oneVarJSONString
	}
	// if Address is empty, we will spawn the unikernel without networking
	if r.Net.Address != "" {
		netJSON, err := json.Marshal(r.Net)
		if err != nil {
			return "", err
		}
		netJSONString = "\"net\":"
		netJSONString += string(netJSON)
	}
	// if Source is empty, we will spawn the unikernel without a block device
	if r.Blk.Source != "" {
		blkJSON, err := json.Marshal(r.Blk)
		if err != nil {
			return "", err
		}
		blkJSONString = "\"blk\":"
		blkJSONString += string(blkJSON)
	}
	finalJSONString := strings.TrimSuffix(cmdJSONString, "}")
	if envJSONString != "" {
		finalJSONString += "," + envJSONString
	}
	if netJSONString != "" {
		finalJSONString += "," + netJSONString
	}
	if blkJSONString != "" {
		finalJSONString += "," + blkJSONString
	}
	finalJSONString += "}"
	return finalJSONString, nil
}

func (r *Rumprun) SupportsBlock() bool {
	return true
}

func (r *Rumprun) SupportsFS(fsType string) bool {
	switch fsType {
	case "ext2":
		return true
	default:
		return false
	}
}

func (r *Rumprun) MonitorNetCli(ifName string, mac string) string {
	switch r.Monitor {
	case "hvt", "spt":
		netOption := "--net:tap=" + ifName
		netOption += " --net-mac:tap=" + mac
		return netOption
	default:
		return ""
	}
}

func (r *Rumprun) MonitorBlockCli() []types.MonitorBlockArgs {
	switch r.Monitor {
	case "hvt", "spt":
		// TODO: Explore options for multiple block devices in Rumprun
		// over Solo5-spt and Solo5-hvt. Solo5 expects to use as an ID
		// a specific name which the guest is also aware of in order to
		// attach the respective block. As a result, urunc needs to know
		// the correct ID to set, which is not straightforward. Therefore,
		// there are two options. Either we read the Solo5 manifest or,
		// we require specific IDs. Till we decide about that, we will
		// use a single block device only for the rootfs of Rumprun.
		return []types.MonitorBlockArgs{
			{
				ID:   "rootfs",
				Path: r.Blk.HostPath,
			},
		}
	default:
		return nil
	}
}

// Rumprun can execute only on top of Solo5 and currently there
// are no generic Solo5-specific arguments that Rumprun requires
func (r *Rumprun) MonitorCli() types.MonitorCliArgs {
	return types.MonitorCliArgs{}
}

func (r *Rumprun) Init(data types.UnikernelParams) error {
	// if Net.Mask is empty, there is no network support
	if data.Net.Mask != "" {
		// FIXME: in the case of rumprun & k8s, we need to identify
		// the reason that networking is not working properly.
		// One reason could be that the gw is in different subnet
		// than the IP of the unikernel.
		// For that reason, we might need to set the mask to an
		// inclusive value (e.g. 0 or 1).
		// However, further exploration of this issue is necessary.
		mask, err := subnetMaskToCIDR(SubnetMask125)
		if err != nil {
			return err
		}
		r.Net.Interface = "ukvmif0"
		r.Net.Cloner = "True"
		r.Net.Type = "inet"
		r.Net.Method = "static"
		r.Net.Address = data.Net.IP
		r.Net.Mask = fmt.Sprintf("%d", mask)
		r.Net.Gateway = data.Net.Gateway
	} else {
		// Set address to empty string so we can know that no network
		// was specified.
		r.Net.Address = ""
	}

	if len(data.Block) > 0 {
		r.Blk.Source = "etfs"
		r.Blk.Path = "/dev/ld0a"
		r.Blk.FsType = "blk"
		r.Blk.Mountpoint = data.Block[0].MountPoint
		r.Blk.HostPath = data.Block[0].Source
	} else {
		// Set source to empty string so we can know that no block
		// was specified.
		r.Blk.Source = ""
	}

	r.Command = strings.Join(data.CmdLine, " ")
	r.Monitor = data.Monitor
	r.Envs = data.EnvVars

	return nil
}

func newRumprun() *Rumprun {
	rumprunStruct := new(Rumprun)

	return rumprunStruct
}
