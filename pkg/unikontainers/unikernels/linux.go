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

// The Linux unikernel builder is platform-neutral: it turns urunc's unikernel
// config into a Linux kernel command line (root=, console=, ip=, urunit
// config) and is shared by the Linux orchestration engine and the darwin
// runner so both produce an identical boot line for the same image.

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
	envStartMarker   string = "UES"
	envEndMarker     string = "UEE"
	lpcStartMarker   string = "UCS" // Linux process config start marker
	lpcEndMarker     string = "UCE" // Linux process config end marker
	blkStartMarker   string = "UBS" // Block-based mounts start marker
	blkEndMarker     string = "UBE" // Block-based mounts end marker
)

type Linux struct {
	App           string
	Command       string
	Monitor       string
	Env           []string
	Net           LinuxNet
	Blk           []types.BlockDevParams
	RootFsType    string
	InitrdConf    bool
	ProcConfig    types.ProcessConfig
	ContainerBoot bool
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
	switch l.Monitor {
	case "vz", "qemu":
		// Both monitors provide a virtio console (hvc0). Prefer it over
		// the emulated UART, where every output byte costs a VM exit.
		// With no UART in use, also skip the 8250 driver probing at
		// boot. Requires CONFIG_VIRTIO_CONSOLE=y in the guest kernel.
		consoleStr = "console=hvc0 8250.nr_uarts=0"
	default:
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
	if l.ContainerBoot {
		// The per-container environment is carried in /urunc-env in the boot
		// initrd, keeping values intact and off the kernel command line.
	} else if !l.InitrdConf {
		for _, eVar := range l.Env {
			bootParams += " " + eVar
		}
	} else {
		if l.RootFsType == "initrd" {
			bootParams += " URUNIT_CONFIG="
			bootParams += urunitConfPath
		} else {
			bootParams += " retain_initrd URUNIT_CONFIG="
			bootParams += retainInitrdPath
		}
	}
	if !IsIPInSubnet(l.Net) {
		bootParams += " URUNIT_DEFROUTE=1"
	}
	if l.ContainerBoot {
		rdinit = "rd"
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
	case "cloud-hypervisor", "hvi":
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

func (l *Linux) MonitorCli() types.MonitorCliArgs {
	switch l.Monitor {
	case "qemu":
		extraCliArgs := types.MonitorCliArgs{
			OtherArgs: []string{"-no-reboot", "-nodefaults"},
		}
		if l.InitrdConf && l.RootFsType != "initrd" {
			extraCliArgs.ExtraInitrd = urunitConfPath
		}
		return extraCliArgs
	case "firecracker", "cloud-hypervisor":
		if l.InitrdConf && l.RootFsType != "initrd" {
			return types.MonitorCliArgs{
				ExtraInitrd: urunitConfPath,
			}
		}
		return types.MonitorCliArgs{}
	default:
		return types.MonitorCliArgs{}
	}
}

func (l *Linux) Init(data types.UnikernelParams) error {
	err := l.parseCmdLine(data.CmdLine)
	if err != nil {
		return err
	}

	l.configureNetwork(data.Net)
	l.Blk = data.Block
	l.RootFsType = data.Rootfs.Type
	l.Env = data.EnvVars
	l.Monitor = data.Monitor
	l.ProcConfig = data.ProcConf
	l.ContainerBoot = data.ContainerBoot
	if l.ContainerBoot {
		if l.Command != "" {
			l.Command = l.App + " " + l.Command
		} else {
			l.Command = l.App
		}
		l.App = "/init"
	}

	// if the application contains urunit, then we assume
	// that the init process is based on our urunit
	// and hence it can handle the information we pass to
	// it through initrd.
	l.InitrdConf = strings.Contains(l.App, "urunit")
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

	// Wrap multi-word arguments in quotes for urunit
	normalizedArgs := make([]string, len(cmdLine))
	for i, arg := range cmdLine {
		arg = strings.TrimSpace(arg)
		if strings.Contains(arg, " ") {
			normalizedArgs[i] = "'" + arg + "'"
		} else {
			normalizedArgs[i] = arg
		}
	}

	l.App = normalizedArgs[0]
	if len(normalizedArgs) > 1 {
		l.Command = strings.Join(normalizedArgs[1:], " ")
	} else {
		l.Command = ""
	}

	return nil
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
	if l.RootFsType == "initrd" {
		var initrdToUpdate string
		initrdToUpdate, err = securejoin.SecureJoin(constants.ContainerRootfsMountPath, rfs.Path)
		if err != nil {
			return fmt.Errorf("failed to setup urunit config: %w", err)
		}
		err = initrd.AddFileToInitrd(initrdToUpdate, urunitConfig, urunitConfPath)
	} else {
		err = createFile(urunitConfPath, urunitConfig)
	}

	if err != nil {
		return fmt.Errorf("failed to setup urunit config: %w", err)
	}

	return nil
}

// buildEnvConfig creates the environment configuration content for urunit.
// BuildUrunitConfig renders a urunit configuration blob from the container's
// environment, process identity and auxiliary block mounts, in urunit's wire
// format (UES/UEE environment, UCS/UCE process config, UBS/UBE block mounts).
//
// Exported so platform runners outside this package — notably the darwin
// product, which stages the rootfs and writes this file itself at
// platform-specific points — generate the exact config urunit expects instead
// of duplicating the format.
func BuildUrunitConfig(env []string, proc types.ProcessConfig, blk []types.BlockDevParams, monitor string) string {
	// Format: UES\n<env1>\n<env2>\n...\nUEE\n
	var sb strings.Builder
	sb.WriteString(envStartMarker)
	sb.WriteString("\n")
	if len(env) > 0 {
		sb.WriteString(strings.Join(env, "\n"))
		sb.WriteString("\n")
	}
	sb.WriteString(envEndMarker)
	sb.WriteString("\n")
	sb.WriteString(lpcStartMarker)
	sb.WriteString("\n")
	sb.WriteString("UID:")
	sb.WriteString(strconv.FormatUint(uint64(proc.UID), 10))
	sb.WriteString("\n")
	sb.WriteString("GID:")
	sb.WriteString(strconv.FormatUint(uint64(proc.GID), 10))
	sb.WriteString("\n")
	sb.WriteString("WD:")
	sb.WriteString(proc.WorkDir)
	sb.WriteString("\n")
	sb.WriteString(lpcEndMarker)
	sb.WriteString("\n")
	sb.WriteString(blkStartMarker)
	sb.WriteString("\n")
	for _, b := range blk {
		if b.ID == "rootfs" {
			continue
		}
		sb.WriteString("ID:")
		if monitor == "firecracker" {
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

func (l *Linux) buildUrunitConfig() string {
	return BuildUrunitConfig(l.Env, l.ProcConfig, l.Blk, l.Monitor)
}

func newLinux() *Linux {
	linuxStruct := new(Linux)
	return linuxStruct
}
