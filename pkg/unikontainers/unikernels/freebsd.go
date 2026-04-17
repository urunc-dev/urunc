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
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const (
	FreeBSDUnikernel string = "freebsd"
	netStartMarker   string = "UNS" // Net configuration start marker
	netEndMarker     string = "UNE" // Net configuration end marker
	paddingMarker    string = "PAD" // Padding bytes
)

type FreeBSD struct {
	Command          []string
	Monitor          string
	Env              []string
	BlockImgAsRootfs bool
	Net              FreeBSDNet
	Block            []types.BlockDevParams
	ProcConfig       types.ProcessConfig
}

type FreeBSDNet struct {
	Address string
	Gateway string
	Mask    string
}

func (f *FreeBSD) CommandString() (string, error) {
	if f.BlockImgAsRootfs {
		return "vfs.root.mountfrom=ext2fs:/dev/vtbd0", nil
	}
	return "vfs.root.mountfrom=/dev/vtbd0", nil
}

func (f *FreeBSD) SupportsBlock() bool {
	return true
}

func (f *FreeBSD) SupportsFS(fsType string) bool {
	switch fsType {
	case "ext2":
		return true
	default:
		return false
	}
}

func (f *FreeBSD) MonitorNetCli(_ string, _ string) string {
	// TODO: Commenting out, since FreeBSd does not work properly over Qemu microVM
	// switch f.Monitor {
	//  TODO: Commenting out, since it is not working properly
	// case "qemu":
	// 	netOption := " -netdev tap,id=net0,script=no,downscript=no,ifname=" + ifName
	// 	netOption += " -device virtio-net-device,netdev=net0,mac=" + mac
	// 	return netOption
	// default:
	// 	return ""
	// }
	return ""
}

func (f *FreeBSD) MonitorBlockCli() []types.MonitorBlockArgs {
	if len(f.Block) == 0 {
		return nil
	}
	blkArgs := make([]types.MonitorBlockArgs, 0, len(f.Block))
	switch f.Monitor {
	// TODO: Commenting out, since it is not working properly
	// case "qemu":
	// 	for _, aBlock := range f.Block {
	// 		bcli1 := fmt.Sprintf(" -device virtio-blk-device,serial=%s,drive=%s", aBlock.ID, aBlock.ID)
	// 		bcli2 := fmt.Sprintf(" -drive format=raw,if=none,id=%s,file=%s", aBlock.ID, aBlock.Source)
	// 		blkArgs = append(blkArgs, types.MonitorBlockArgs{
	// 			ExactArgs: bcli1 + bcli2,
	// 		})
	// 	}
	case "firecracker":
		for _, aBlock := range f.Block {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ID:   "FC" + aBlock.ID,
				Path: aBlock.Source,
			})
		}
		blkArgs = append(blkArgs, types.MonitorBlockArgs{
			ID:   "FC_URUNIT_CONFIG",
			Path: urunitConfPath,
		})
	default:
		return nil
	}

	return blkArgs
}

func (f *FreeBSD) MonitorCli() types.MonitorCliArgs {
	// TODO: Commenting out, since FreeBSd does not work properly over Qemu microVM
	// switch f.Monitor {
	// case "qemu":
	// 	monArgs := " -M microvm,rtc=on,acpi=off,pic=off,accel=kvm -global virtio-mmio.force-legacy=false -no-reboot -display none -nodefaults -serial stdio"
	// 	return types.MonitorCliArgs{
	// 		OtherArgs: monArgs,
	// 	}
	// default:
	// 	return types.MonitorCliArgs{}
	// }
	return types.MonitorCliArgs{}
}

func (f *FreeBSD) Init(data types.UnikernelParams) error {
	// if Mask is empty, there is no network support
	if data.Net.Mask != "" {
		f.Net.Address = data.Net.IP
		f.Net.Gateway = data.Net.Gateway
		f.Net.Mask = data.Net.Mask
	}
	f.Block = data.Block
	f.Env = data.EnvVars
	f.Command = data.CmdLine
	f.Monitor = data.Monitor
	f.ProcConfig = data.ProcConf
	f.BlockImgAsRootfs = false
	if data.Rootfs.MountedPath != "" {
		f.BlockImgAsRootfs = true
	}

	err := f.setupUrunitConfig(data.Rootfs)
	if err != nil {
		return err
	}

	return nil
}

// setupUrunitConfig creates the urunit configuration file with environment variables.
func (f *FreeBSD) setupUrunitConfig(rfs types.RootfsParams) error {
	urunitConfig := f.buildUrunitConfig()

	urunitConfigFile := filepath.Join(rfs.MonRootfs, urunitConfPath)
	err := createFile(urunitConfigFile, urunitConfig)
	if err != nil {
		return fmt.Errorf("failed to setup urunit config: %w", err)
	}

	return nil
}

// buildEnvConfig creates the environment configuration content for urunit.
func (f *FreeBSD) buildUrunitConfig() string {
	// Format: UES\n<env1>\n<env2>\n...\nUEE\n
	var sb strings.Builder
	sb.WriteString(envStartMarker)
	sb.WriteString("\n")
	if len(f.Env) > 0 {
		sb.WriteString(strings.Join(f.Env, "\n"))
		sb.WriteString("\n")
	}
	sb.WriteString(envEndMarker)
	sb.WriteString("\n")
	sb.WriteString(lpcStartMarker)
	sb.WriteString("\n")
	sb.WriteString("UID:")
	sb.WriteString(strconv.FormatUint(uint64(f.ProcConfig.UID), 10))
	sb.WriteString("\n")
	sb.WriteString("GID:")
	sb.WriteString(strconv.FormatUint(uint64(f.ProcConfig.GID), 10))
	sb.WriteString("\n")
	sb.WriteString("WD:")
	sb.WriteString(f.ProcConfig.WorkDir)
	sb.WriteString("\n")
	sb.WriteString("ARC:")
	sb.WriteString(strconv.FormatUint(uint64(len(f.Command)), 10))
	sb.WriteString("\n")
	for _, c := range f.Command {
		sb.WriteString("ARV:")
		sb.WriteString(c)
		sb.WriteString("\n")
	}
	sb.WriteString(lpcEndMarker)
	sb.WriteString("\n")
	sb.WriteString(blkStartMarker)
	sb.WriteString("\n")
	for _, b := range f.Block {
		if b.ID == "rootfs" {
			continue
		}
		sb.WriteString("ID:")
		if f.Monitor == "firecracker" {
			sb.WriteString("FC")
		}
		sb.WriteString(b.ID)
		sb.WriteString("\n")
		sb.WriteString("MP:")
		sb.WriteString(b.MountPoint)
		sb.WriteString("\n")
	}
	sb.WriteString(blkEndMarker)
	sb.WriteString("\n")
	sb.WriteString(netStartMarker)
	sb.WriteString("\n")
	sb.WriteString("IP:")
	sb.WriteString(f.Net.Address)
	sb.WriteString("\n")
	sb.WriteString("GW:")
	sb.WriteString(f.Net.Gateway)
	sb.WriteString("\n")
	sb.WriteString("MSK:")
	sb.WriteString(f.Net.Mask)
	sb.WriteString("\n")
	sb.WriteString(netEndMarker)
	sb.WriteString("\n")
	for i := 0; i < 128; i++ {
		sb.WriteString(paddingMarker)
		sb.WriteString("\n")
	}
	return sb.String()
}

func newFreeBSD() *FreeBSD {
	freebsdStruct := new(FreeBSD)
	return freebsdStruct
}
