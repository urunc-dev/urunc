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
	"net"
	"strconv"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/urunc-dev/urunc/internal/constants"
	"github.com/urunc-dev/urunc/pkg/unikontainers/initrd"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const (
	LinuxUnikernel   string = "linux"
	urunitConfPath   string = "/urunit.conf"
	retainInitrdPath string = "/sys/firmware/initrd"
	// containerBootInit is the early userspace of a generic container boot: the
	// boot initrd's /init, which mounts the shared container rootfs, stages
	// urunit and the exec agent (at /run/urunc/urunit-agent, where urunit
	// starts it from) and switch_roots into urunit with the container's
	// command.
	containerBootInit string = "/init"
	envStartMarker    string = "UES"
	envEndMarker      string = "UEE"
	lpcStartMarker    string = "UCS" // Linux process config start marker
	lpcEndMarker      string = "UCE" // Linux process config end marker
	blkStartMarker    string = "UBS" // Block-based mounts start marker
	blkEndMarker      string = "UBE" // Block-based mounts end marker
)

type Linux struct {
	App        string
	Command    string
	Monitor    string
	Env        []string
	Net        LinuxNet
	Blk        []types.BlockDevParams
	RootFsType string
	InitrdConf bool
	ProcConfig types.ProcessConfig
	// ContainerBoot marks a generic container boot (see types.UnikernelParams).
	// The guest then boots BootInitrd, the host boot initrd mounted into the
	// monitor rootfs, through a private copy carrying the urunit configuration.
	ContainerBoot bool
	BootInitrd    string
}

type LinuxNet struct {
	Address string
	Gateway string
	Mask    string
}

func IsIPInSubnet(ln LinuxNet) bool {
	ip := net.ParseIP(ln.Address)
	gw := net.ParseIP(ln.Gateway)
	mask := net.IPMask(net.ParseIP(ln.Mask).To4())
	subnet := gw.Mask(mask)

	return ip.Mask(mask).Equal(subnet)
}

func (l *Linux) CommandString() (string, error) {
	rdinit := ""
	bootParams := "panic=-1"

	// TODO: Check if this check causes any performance drop
	// or explore alternative implementations
	consoleStr := ""
	// TODO: Check under which conditions console should be set to
	// ttyS0 or ttyAMA0. Currently, we have noticed that FC requires ttyS0
	// for both amd64 and arm64.
	if l.Monitor == "qemu" {
		// QEMU provides a virtio console (hvc0). Prefer it over the
		// emulated UART, where every output byte costs a VM exit. With
		// no UART in use, also skip the 8250 driver probing at boot.
		// Requires CONFIG_VIRTIO_CONSOLE=y in the guest kernel.
		consoleStr = "console=hvc0 8250.nr_uarts=0"
	} else {
		consoleStr = "console=ttyS0"
	}
	bootParams += " " + consoleStr

	switch l.RootFsType {
	case "block":
		rootParams := "root=/dev/vda rw"
		bootParams += " " + rootParams
	case "initrd":
		rootParams := "root=/dev/ram0 rw"
		rdinit = "rd"
		bootParams += " " + rootParams
	case "9pfs":
		rootParams := "root=fs0 rw rootfstype=9p rootflags="
		rootParams += "trans=virtio,version=9p2000.L,msize=5000000,cache=mmap,posixacl"
		bootParams += " " + rootParams
	case "virtiofs":
		rootParams := "root=fs0 rw rootfstype=virtiofs"
		bootParams += " " + rootParams
	}
	if l.Net.Address != "" {
		netParams := fmt.Sprintf("ip=%s::%s:%s:urunc:eth0:off",
			l.Net.Address,
			l.Net.Gateway,
			l.Net.Mask)
		bootParams += " " + netParams
	}
	switch {
	case !l.InitrdConf:
		for _, eVar := range l.Env {
			bootParams += " " + eVar
		}
	case l.ContainerBoot:
		// The environment travels in the urunit configuration appended to the
		// boot initrd. Its /init copies that file into the new root and points
		// urunit at it with URUNIT_CONFIG, so nothing is needed here. The root
		// parameters above stay: /init mounts the rootfs they describe itself,
		// since with an initramfs the kernel leaves that to early userspace.
		rdinit = "rd"
	case l.RootFsType == "initrd":
		bootParams += " URUNIT_CONFIG="
		bootParams += urunitConfPath
	default:
		bootParams += " retain_initrd URUNIT_CONFIG="
		bootParams += retainInitrdPath
	}
	if !IsIPInSubnet(l.Net) {
		bootParams += " URUNIT_DEFROUTE=1"
	}
	if l.App != "" {
		initParams := rdinit + "init=" + l.App + " -- " + l.Command
		bootParams += " " + initParams
	}

	return bootParams, nil
}

func (l *Linux) SupportsBlock() bool {
	return true
}

func (l *Linux) SupportsFS(fsType string) bool {
	switch fsType {
	case "ext2":
		return true
	case "ext3":
		return true
	case "ext4":
		return true
	case "9pfs":
		return true
	case "virtiofs":
		return true
	default:
		return false
	}
}

func (l *Linux) MonitorNetCli(_ string, _ string) []string {
	return nil
}

func (l *Linux) MonitorBlockCli() []types.MonitorBlockArgs {
	if len(l.Blk) == 0 {
		return nil
	}
	blkArgs := make([]types.MonitorBlockArgs, 0, len(l.Blk))
	switch l.Monitor {
	case "qemu":
		for _, aBlock := range l.Blk {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ExactArgs: []string{
					"-device", fmt.Sprintf("virtio-blk-pci,serial=%s,drive=%s", aBlock.ID, aBlock.ID),
					"-drive", fmt.Sprintf("format=raw,if=none,id=%s,file=%s", aBlock.ID, aBlock.Source),
				},
			})
		}
	case "firecracker":
		for _, aBlock := range l.Blk {
			id := aBlock.ID
			if l.Monitor == "firecracker" {
				id = "FC" + aBlock.ID
			}
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ID:   id,
				Path: aBlock.Source,
			})
		}
	case "cloud-hypervisor":
		for _, aBlock := range l.Blk {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ID:   aBlock.ID,
				Path: aBlock.Source,
			})
		}
	default:
		return nil
	}

	return blkArgs
}

// MonitorSharedfsCli exposes the 9p shared rootfs over QEMU's PCI transport.
func (l *Linux) MonitorSharedfsCli(fsType string, path string) []string {
	if l.Monitor != "qemu" || fsType != "9pfs" {
		return nil
	}
	return []string{
		"-fsdev", "local,id=rootfs9p,security_model=none,multidevs=remap,path=" + path,
		"-device", "virtio-9p-pci,fsdev=rootfs9p,mount_tag=fs0",
	}
}

func (l *Linux) MonitorCli() types.MonitorCliArgs {
	switch l.Monitor {
	case "qemu":
		extraCliArgs := types.MonitorCliArgs{
			OtherArgs: []string{"-no-reboot", "-nodefaults"},
		}
		if l.urunitConfAsInitrd() {
			extraCliArgs.ExtraInitrd = urunitConfPath
		}
		return extraCliArgs
	case "firecracker", "cloud-hypervisor":
		if l.urunitConfAsInitrd() {
			return types.MonitorCliArgs{
				ExtraInitrd: urunitConfPath,
			}
		}
		return types.MonitorCliArgs{}
	default:
		return types.MonitorCliArgs{}
	}
}

// urunitConfAsInitrd reports whether the urunit configuration is handed to the
// guest as the initrd itself (read back through /sys/firmware/initrd). That is
// the case for a urunit guest with a non-initrd rootfs, except for a generic
// container boot, which has a real boot initrd and appends the configuration
// to it.
func (l *Linux) urunitConfAsInitrd() bool {
	return l.InitrdConf && l.RootFsType != "initrd" && !l.ContainerBoot
}

func (l *Linux) Init(data types.UnikernelParams) error {
	l.ContainerBoot = data.ContainerBoot
	l.BootInitrd = data.InitrdPath

	var err error
	if l.ContainerBoot {
		err = l.parseContainerBootCmdLine(data.CmdLine)
	} else {
		err = l.parseCmdLine(data.CmdLine)
	}
	if err != nil {
		return err
	}

	l.configureNetwork(data.Net)
	l.Blk = data.Block
	l.RootFsType = data.Rootfs.Type
	l.Env = data.EnvVars
	l.Monitor = data.Monitor
	l.ProcConfig = data.ProcConf

	// if the application contains urunit, then we assume
	// that the init process is based on our urunit
	// and hence it can handle the information we pass to
	// it through initrd. A generic container boot always hands over to the
	// urunit shipped in its boot initrd.
	l.InitrdConf = l.ContainerBoot || strings.Contains(l.App, "urunit")
	if l.InitrdConf {
		err := l.setupUrunitConfig(data.Rootfs)
		if err != nil {
			return err
		}
	}

	return nil
}

// parseCmdLine extracts the application and command from command line arguments.
// Multi-word arguments are wrapped in single quotes for urunit compatibility.
func (l *Linux) parseCmdLine(cmdLine []string) error {
	if len(cmdLine) == 0 {
		return fmt.Errorf("no init was specified")
	}

	normalizedArgs := normalizeArgs(cmdLine)
	l.App = normalizedArgs[0]
	if len(normalizedArgs) > 1 {
		l.Command = strings.Join(normalizedArgs[1:], " ")
	} else {
		l.Command = ""
	}

	return nil
}

// parseContainerBootCmdLine sets up the command line of a generic container
// boot: the boot initrd's /init is the guest init and the whole container
// command (entrypoint and arguments) is passed to it, to be handed over to
// urunit after the switch_root.
func (l *Linux) parseContainerBootCmdLine(cmdLine []string) error {
	if len(cmdLine) == 0 {
		return fmt.Errorf("no command was specified for the container")
	}

	l.App = containerBootInit
	l.Command = strings.Join(normalizeArgs(cmdLine), " ")

	return nil
}

// normalizeArgs trims the arguments and wraps multi-word ones in single quotes
// for urunit compatibility.
func normalizeArgs(cmdLine []string) []string {
	normalizedArgs := make([]string, len(cmdLine))
	for i, arg := range cmdLine {
		arg = strings.TrimSpace(arg)
		if strings.Contains(arg, " ") {
			normalizedArgs[i] = "'" + arg + "'"
		} else {
			normalizedArgs[i] = arg
		}
	}

	return normalizedArgs
}

// configureNetwork sets up network parameters.
func (l *Linux) configureNetwork(net types.NetDevParams) {
	l.Net.Address = net.IP
	l.Net.Gateway = net.Gateway
	l.Net.Mask = net.Mask
}

// setupUrunitConfig creates the urunit configuration file with environment variables.
func (l *Linux) setupUrunitConfig(rfs types.RootfsParams) error {
	urunitConfig := l.buildUrunitConfig()

	var err error
	switch {
	case l.ContainerBoot:
		err = l.setupContainerBootInitrd(urunitConfig)
	case l.RootFsType == "initrd":
		var initrdToUpdate string
		initrdToUpdate, err = securejoin.SecureJoin(constants.ContainerRootfsMountPath, rfs.Path)
		if err != nil {
			return fmt.Errorf("failed to setup urunit config: %w", err)
		}
		err = initrd.AddFileToInitrd(initrdToUpdate, urunitConfig, urunitConfPath)
	default:
		err = createFile(urunitConfPath, urunitConfig)
	}

	if err != nil {
		return fmt.Errorf("failed to setup urunit config: %w", err)
	}

	return nil
}

// setupContainerBootInitrd writes the initrd a generic container boot guest
// actually boots: a private copy of the host boot initrd, which is mounted
// read-only into the monitor rootfs and shared by every container booting
// from it, with only the urunit configuration of this container appended as
// /urunit.conf. The initrd's /init copies that file into the new root and
// points urunit at it. Everything else the guest needs (the command line, the
// environment and the network files) reaches it through the kernel command
// line, this configuration and the bind mounts of the shared rootfs.
func (l *Linux) setupContainerBootInitrd(urunitConfig string) error {
	if l.BootInitrd == "" {
		return fmt.Errorf("container boot requires the path of the boot initrd")
	}

	err := copyFile(l.BootInitrd, constants.ContainerBootGuestInitrdPath)
	if err != nil {
		return fmt.Errorf("could not copy the boot initrd: %w", err)
	}

	return initrd.AddFileToInitrd(constants.ContainerBootGuestInitrdPath, urunitConfig, urunitConfPath)
}

// buildEnvConfig creates the environment configuration content for urunit.
func (l *Linux) buildUrunitConfig() string {
	// Format: UES\n<env1>\n<env2>\n...\nUEE\n
	var sb strings.Builder
	sb.WriteString(envStartMarker)
	sb.WriteString("\n")
	if len(l.Env) > 0 {
		sb.WriteString(strings.Join(l.Env, "\n"))
		sb.WriteString("\n")
	}
	sb.WriteString(envEndMarker)
	sb.WriteString("\n")
	sb.WriteString(lpcStartMarker)
	sb.WriteString("\n")
	sb.WriteString("UID:")
	sb.WriteString(strconv.FormatUint(uint64(l.ProcConfig.UID), 10))
	sb.WriteString("\n")
	sb.WriteString("GID:")
	sb.WriteString(strconv.FormatUint(uint64(l.ProcConfig.GID), 10))
	sb.WriteString("\n")
	sb.WriteString("WD:")
	sb.WriteString(l.ProcConfig.WorkDir)
	sb.WriteString("\n")
	sb.WriteString(lpcEndMarker)
	sb.WriteString("\n")
	sb.WriteString(blkStartMarker)
	sb.WriteString("\n")
	for _, b := range l.Blk {
		if b.ID == "rootfs" {
			continue
		}
		sb.WriteString("ID:")
		if l.Monitor == "firecracker" {
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
	return sb.String()
}

func newLinux() *Linux {
	linuxStruct := new(Linux)
	return linuxStruct
}
