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
	freebsdRootBlock string = "/dev/vtbd0"
	// blockSectorSize is the granularity at which a raw file is exposed by
	// the monitor as a virtio-blk device. Firecracker silently hides a trailing
	// partial sector, so the config file is padded to a multiple of it.
	blockSectorSize int    = 512
	freeBSDufs      string = "ufs"
	freeBSDext2fs   string = "ext2fs"
	p9MountTag      string = "fs0"
)

type FreeBSD struct {
	Command     []string
	InitPath    string
	WithUrunit  bool
	Monitor     string
	Env         []string
	RootFsType  string
	BlockFsType string
	Net         FreeBSDNet
	Block       []types.BlockDevParams
	ProcConfig  types.ProcessConfig
}

type FreeBSDNet struct {
	Address string
	Gateway string
	Mask    string
}

// CommandString builds the FreeBSD boot arguments. On the PVH boot mode every
// name=value token becomes a kernel environment variable. Therefore, use this
// mechanism to pass the location of the urunit configuration to the guest.
// Boot arguments must not contain commas, which act as token delimiters.
func (f *FreeBSD) CommandString() (string, error) {
	var params []string

	if f.RootFsType == "9pfs" {
		params = append(params, "vfs.root.mountfrom=p9fs:"+p9MountTag)
	} else {
		params = append(params, "vfs.root.mountfrom="+f.BlockFsType+":"+freebsdRootBlock)
	}

	if f.InitPath != "" {
		params = append(params, "init_path="+f.InitPath)
	}

	// If init is urunit then set the URUNIT_* variables
	if f.WithUrunit {
		// urunit reads its configuration from a raw virtio-blk device
		// attached after all other block devices. In the case of
		// block-based rootfs the other block devices are the rootfs
		// and the block-based volumes. In the case of 9pfs-based
		// rootfs there is no other block device and therefore
		// len(f.Block) will be 0.
		params = append(params, "URUNIT_CONFIG=/dev/vtbd"+strconv.Itoa(len(f.Block)))
		if f.Net.Address != "" {
			ln := LinuxNet{Address: f.Net.Address, Gateway: f.Net.Gateway, Mask: f.Net.Mask}
			if !IsIPInSubnet(ln) {
				params = append(params, "URUNIT_DEFROUTE=1")
			}
		}
	}

	return strings.Join(params, " "), nil
}

func (f *FreeBSD) SupportsBlock() bool {
	return true
}

func (f *FreeBSD) SupportsFS(fsType string) bool {
	switch fsType {
	case "ext2":
		return true
	case "9pfs":
		return true
	default:
		return false
	}
}

func (f *FreeBSD) MonitorNetCli(ifName string, mac string) []string {
	switch f.Monitor {
	case "qemu":
		// QEMU microvm has no PCI bus: use the virtio-mmio transport.
		return []string{
			"-netdev", "tap,id=net0,script=no,downscript=no,ifname=" + ifName,
			"-device", "virtio-net-device,netdev=net0,mac=" + mac,
		}
	default:
		return nil
	}
}

func (f *FreeBSD) MonitorBlockCli() []types.MonitorBlockArgs {
	// When urunit is the init, its configuration is passed as one extra block
	// device after a block-based rootfs and block-based volumes. This holds
	// for a 9pfs rootfs too: the rootfs is served over 9p, but the urunit
	// config is still a raw virtio-blk device.
	blockArgsCap := len(f.Block)
	if f.WithUrunit {
		blockArgsCap++
	}
	if blockArgsCap == 0 {
		return nil
	}
	blkArgs := make([]types.MonitorBlockArgs, 0, blockArgsCap)
	switch f.Monitor {
	case "qemu":
		for _, aBlock := range f.Block {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ExactArgs: qemuMMIOBlock(aBlock.ID, aBlock.Source),
			})
		}
		if f.WithUrunit {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ExactArgs: qemuMMIOBlock("urunit", urunitConfPath),
			})
		}
	case "firecracker":
		for _, aBlock := range f.Block {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ID:   "FC" + aBlock.ID,
				Path: aBlock.Source,
			})
		}
		if f.WithUrunit {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ID:   "FC_URUNIT_CONFIG",
				Path: urunitConfPath,
			})
		}
	default:
		return nil
	}

	return blkArgs
}

// MonitorSharedfsCli exposes the 9p shared rootfs over the virtio-mmio
// transport only for Qemu's microvm.  It sets multidevs=remap: since the
// shared container rootfs usually spans several host devices (overlay layers,
// bind-mounted files and dirs).
func (f *FreeBSD) MonitorSharedfsCli(fsType string, path string) []string {
	if f.Monitor != "qemu" || fsType != "9pfs" {
		return nil
	}
	fsdev := "local,id=rootfs9p,security_model=none,multidevs=remap,path=" + path
	dev := "virtio-9p-device,fsdev=rootfs9p,mount_tag=" + p9MountTag
	return []string{"-fsdev", fsdev, "-device", dev}
}

func (f *FreeBSD) MonitorCli() types.MonitorCliArgs {
	switch f.Monitor {
	case "qemu":
		// NOTE: pic=off makes QEMU (8.2/9.1) crash right after FreeBSD
		// initializes the I/O APIC. ACPI must be on, since FreeBSD uses
		// the ACPI tables instead of the (deprecated) MP table.
		monArgs := []string{"-M", "microvm,acpi=on,pic=on,pit=on,rtc=on", "-nodefaults", "-no-reboot"}
		return types.MonitorCliArgs{
			OtherArgs: monArgs,
		}
	default:
		return types.MonitorCliArgs{}
	}
}

func (f *FreeBSD) Init(data types.UnikernelParams) error {
	// if Mask is empty, there is no network
	if data.Net.Mask != "" {
		f.Net.Address = data.Net.IP
		f.Net.Gateway = data.Net.Gateway
		f.Net.Mask = data.Net.Mask
	}
	f.Block = data.Block
	f.Env = data.EnvVars
	f.WithUrunit = usesUrunit(data.CmdLine)
	// init_path is the first command token verbatim
	if len(data.CmdLine) > 0 {
		f.InitPath = data.CmdLine[0]
		f.Command = data.CmdLine[1:]
	}
	f.Monitor = data.Monitor
	f.ProcConfig = data.ProcConf
	f.RootFsType = data.Rootfs.Type
	switch f.RootFsType {
	case "block":
		f.BlockFsType = freeBSDufs
		if data.Rootfs.MountedPath != "" {
			f.BlockFsType = freeBSDext2fs
		}
	case "9pfs":
		// The rootfs is served over 9p; nothing extra to configure here.
	default:
		return fmt.Errorf("freebsd: unsupported rootfs type %q: only block and 9pfs are supported", f.RootFsType)
	}

	// Only provision the urunit configuration when urunit is the init.
	if f.WithUrunit {
		err := f.setupUrunitConfig()
		if err != nil {
			return err
		}
	}

	return nil
}

// usesUrunit reports whether the entrypoint of the container was urunit.
// It does that simply checking if the command to execute is urunit.
func usesUrunit(cmdLine []string) bool {
	return len(cmdLine) > 0 && filepath.Base(cmdLine[0]) == "urunit"
}

func qemuMMIOBlock(id string, path string) []string {
	return []string{
		"-device", fmt.Sprintf("virtio-blk-device,serial=%s,drive=%s", id, id),
		"-drive", fmt.Sprintf("format=raw,if=none,id=%s,file=%s", id, path),
	}
}

// setupUrunitConfig writes the urunit configuration file into the monitor
// rootfs at urunitConfPath. The monitor exposes that file to the guest as a
// raw virtio-blk device.
func (f *FreeBSD) setupUrunitConfig() error {
	urunitConfig := f.buildUrunitConfig()
	err := createFile(urunitConfPath, urunitConfig)
	if err != nil {
		return fmt.Errorf("failed to setup urunit config: %w", err)
	}
	return nil
}

// buildUrunitConfig creates the configuration content for urunit. Compared to
// Linux guests it also carries the application command (ARC/ARV, since the
// FreeBSD kernel starts init without arguments) and the network configuration
// (UNS/UNE, since FreeBSD has no ip= boot parameter). The result is padded to
// a multiple of the sector size, as it is exposed as a raw block device.
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
	// Pad to a whole number of sectors. The PAD line marks the end of the
	// configuration for readers; the remaining bytes are newlines.
	sb.WriteString(paddingMarker)
	sb.WriteString("\n")
	for sb.Len()%blockSectorSize != 0 {
		sb.WriteString("\n")
	}
	return sb.String()
}

func newFreeBSD() *FreeBSD {
	freebsdStruct := new(FreeBSD)
	return freebsdStruct
}
