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

package hypervisors

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	// hviVirtiofsTag is the tag the Linux boot line mounts as root
	// (root=fs0 rootfstype=virtiofs). Cloud Hypervisor and QEMU use it too.
	hviVirtiofsTag = "fs0"
	// hviVirtiofsSocket is where sharedfsRootfs.preStartCmd tells virtiofsd
	// to listen, inside the monitor's own /tmp.
	hviVirtiofsSocket = "/tmp/vhostqemu"
)

// Hvi runs a Linux guest with the hvi monitor on KVM.
//
// The rootfs arrives as an initrd, as a block device, or as a
// virtio-fs export. For virtio-fs, urunc starts virtiofsd before the monitor
// and hvi connects to the daemon's vhost-user socket, as Cloud Hypervisor
// does. The exec agent is reached through hvi's agent socket, a host unix
// socket that hvi relays to guest vsock port 1024.
type Hvi struct {
	binaryPath string
	binary     string
}

func (h *Hvi) Signal(pid int, signal unix.Signal, _ string) error {
	return signalProcess(pid, signal)
}

// Stop kills the hvi process.
func (h *Hvi) Stop(pid int) error {
	return killProcess(pid)
}

// UsesKVM returns true: hvi is a KVM monitor on Linux.
func (h *Hvi) UsesKVM() bool {
	return true
}

// UsesTUNTAP reports that the guest gets its network through a tap device:
// hvi's built-in stack forwards no traffic, so urunc hands it the tap.
func (h *Hvi) UsesTUNTAP() bool {
	return true
}

// SupportsControlSocket returns false: hvi has no control API.
func (h *Hvi) SupportsControlSocket() bool {
	return false
}

// SupportsSharedfs returns whether hvi can share a host directory of the given
// type. virtio-fs goes through a vhost-user daemon, and there is no 9p device.
func (h *Hvi) SupportsSharedfs(fsType string) bool {
	return fsType == "virtio"
}

// Path returns the path to the hvi binary.
func (h *Hvi) Path() string {
	return h.binaryPath
}

// Ok returns nil. NewVMM has already resolved the binary.
func (h *Hvi) Ok() error {
	return nil
}

// BuildExecCmd assembles the hvi argv.
//
// The kernel has to be one hvi loads directly: a bzImage or an ELF vmlinux on
// x86-64, an Image on arm64. On x86-64 hvi adds console=ttyS0 and one
// virtio_mmio.device= per device to the command line, ahead of the "--" that
// starts the init's arguments.
func (h *Hvi) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	if args.UnikernelPath == "" {
		return nil, fmt.Errorf("hvi: no kernel image to boot")
	}
	// hvi has no device a guest-initiated vsock connection could reach a host
	// socket through, so vAccel over vsock has nowhere to go.
	if args.VAccelType == "vsock" {
		return nil, fmt.Errorf("hvi does not support vAccel over vsock")
	}

	mem := DefaultMemory
	if m := bytesToMiB(args.MemSizeB); m != 0 {
		mem = m
	}
	vcpus := args.VCPUs
	if vcpus == 0 {
		vcpus = 1
	}
	cmd := []string{
		h.Path(), "boot",
		"--kernel", args.UnikernelPath,
		"--mem-mib", strconv.FormatUint(mem, 10),
		"--cpus", strconv.FormatUint(uint64(vcpus), 10),
	}

	// hvi takes one initrd. A urunit guest with a non-initrd rootfs gets its
	// configuration as the initrd, which is the only one it then needs.
	initrd := args.InitrdPath
	if extra := ukernel.MonitorCli().ExtraInitrd; extra != "" {
		if initrd != "" {
			return nil, fmt.Errorf("hvi takes a single initrd, but two were requested: %s and %s", initrd, extra)
		}
		initrd = extra
	}
	if initrd != "" {
		cmd = append(cmd, "--initramfs", initrd)
	}

	var blocks []types.MonitorBlockArgs
	for _, b := range ukernel.MonitorBlockCli() {
		if b.Path != "" {
			blocks = append(blocks, b)
		}
	}
	disks, err := hviLinuxDiskArgs(blocks)
	if err != nil {
		return nil, err
	}
	cmd = append(cmd, disks...)

	// The export is the daemon's. Its directory, write permission and cache
	// policy are virtiofsd options, set in preStartCmd. hvi needs only the
	// socket, and waits up to 60 s for it, so the two may start together.
	if args.Sharedfs.Type == "virtiofs" {
		cmd = append(cmd, "--fs", "tag="+hviVirtiofsTag+",socket="+hviVirtiofsSocket)
	}
	for _, e := range args.VirtiofsExports {
		spec, err := virtiofsExportSpec("hvi", e)
		if err != nil {
			return nil, err
		}
		cmd = append(cmd, "--fs", spec)
	}

	// The guest attaches to the tap urunc created in the container's network
	// namespace, with the MAC the boot line's ip= was derived for.
	if args.Net.TapDev != "" {
		cmd = append(cmd, "--net-tap", args.Net.TapDev)
		if args.Net.MAC != "" {
			cmd = append(cmd, "--net-mac", args.Net.MAC)
		}
	}

	// hvi listens on this socket and relays each connection to the agent's
	// vsock port. The guest is always CID 3, so AgentVsockCID only switches
	// the socket on.
	if args.AgentVsockCID != 0 && args.AgentVsockUDS != "" {
		cmd = append(cmd, "--agent-sock", args.AgentVsockUDS)
	}

	if !args.Seccomp {
		cmd = append(cmd, "--no-sandbox")
	}
	if args.ContainerID != "" {
		cmd = append(cmd, "--sandbox-id", args.ContainerID)
	}
	if other := ukernel.MonitorCli().OtherArgs; len(other) > 0 {
		cmd = append(cmd, other...)
	}
	cmd = append(cmd, "--cmdline", args.Command)

	vmmLog.WithField("hvi command", cmd).Debug("Ready to execve hvi")
	return cmd, nil
}

// hviLinuxDiskArgs returns the --disk flags for blocks, in order.
//
// A single writable disk with no serial is the block rootfs, the guest's
// /dev/vda, and goes out as a plain path, which every hvi takes. Anything
// else needs the keyed form of hvi-vmm main (590c45d and later): a read-only
// disk, a serial, or more than one disk.
func hviLinuxDiskArgs(blocks []types.MonitorBlockArgs) ([]string, error) {
	if len(blocks) == 1 && !blocks[0].ReadOnly && blocks[0].Serial == "" {
		return []string{"--disk", blocks[0].Path}, nil
	}
	var out []string
	for _, b := range blocks {
		// The option string is split on commas, with no escape.
		if strings.ContainsRune(b.Path, ',') {
			return nil, fmt.Errorf("hvi: block device %q: a comma cannot be passed in --disk", b.Path)
		}
		spec := "path=" + b.Path
		if b.ReadOnly {
			spec += ",readonly=on"
		}
		if b.Serial != "" {
			spec += ",serial=" + b.Serial
		}
		out = append(out, "--disk", spec)
	}
	return out, nil
}

// virtiofsExportSpec returns the --fs value of e, which hvi and Cloud
// Hypervisor spell the same way.
func virtiofsExportSpec(monitor string, e types.VirtiofsExport) (string, error) {
	if e.Tag == "" || e.Socket == "" {
		return "", fmt.Errorf("%s: a virtio-fs export needs a tag and a socket, got %+v", monitor, e)
	}
	if strings.ContainsAny(e.Tag+e.Socket, ",=") {
		return "", fmt.Errorf("%s: virtio-fs export %q: a comma or = cannot be passed in --fs", monitor, e.Tag)
	}
	return "tag=" + e.Tag + ",socket=" + e.Socket, nil
}

// PreExec is a no-op: hvi installs its own seccomp filters.
func (h *Hvi) PreExec(_ types.ExecArgs) error {
	return nil
}
