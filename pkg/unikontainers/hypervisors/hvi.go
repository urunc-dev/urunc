//go:build linux
// +build linux

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
	"os/exec"
	"strconv"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	HviVmm    VmmType = "hvi"
	HviBinary string  = "hvi"

	// telemControlSockEnv is how urunc opts a monitor into serving the
	// out-of-VMM introspection socket. Named for the Firecracker fork that
	// consumes it as an environment variable; hvi takes the same path as a flag.
	telemControlSockEnv = "FIRECRACKER_TELEM_CONTROL_SOCK"
	// introspectIntervalEnv carries the self-introspection cadence in seconds.
	introspectIntervalEnv = "VMI_INTROSPECT_INTERVAL"
)

// Hvi runs a Linux guest with the NOFireAI/hvi monitor. It is a KVM backend
// with initrd and single-block-device support. HVI takes plain CLI arguments,
// so this monitor builds an argv in the same style as the Solo5 and QEMU
// backends. Optional HVI capabilities are enabled only when their corresponding
// execution settings are present.
type Hvi struct {
	binaryPath string
	binary     string
}

func (h *Hvi) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

// Stop kills the hvi process.
func (h *Hvi) Stop(pid int) error {
	return killProcess(pid)
}

// UsesKVM returns true: hvi is a KVM monitor on Linux.
func (h *Hvi) UsesKVM() bool {
	return true
}

// SupportsSharedfs reports whether hvi can share a host directory. It has no
// virtiofs device, so the rootfs has to arrive as a block device or an initrd.
func (h *Hvi) SupportsSharedfs(_ string) bool {
	return false
}

// Path returns the path to the hvi binary.
func (h *Hvi) Path() string {
	return h.binaryPath
}

// Ok checks that the hvi binary is reachable.
func (h *Hvi) Ok() error {
	if _, err := exec.LookPath(HviBinary); err != nil {
		return ErrVMMNotInstalled
	}
	return nil
}

// BuildExecCmd assembles the hvi argv.
//
// hvi wants a bzImage rather than the ELF vmlinux Firecracker loads, so the
// kernel handed to it must be the compressed image; a plain vmlinux is rejected
// at load time with "bad bzImage boot flag".
func (h *Hvi) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	kernel := args.KernelPath
	if kernel == "" {
		kernel = args.UnikernelPath
	}
	if kernel == "" {
		return nil, fmt.Errorf("hvi: no kernel image to boot")
	}

	mem := DefaultMemory
	if args.MemSizeB != 0 {
		if m := bytesToMiB(args.MemSizeB); m != 0 {
			mem = m
		}
	}
	vcpus := args.VCPUs
	if vcpus == 0 {
		vcpus = 1
	}
	cmd := []string{
		h.Path(), "boot",
		"--kernel", kernel,
		"--mem-mib", strconv.FormatUint(mem, 10),
		"--cpus", strconv.FormatUint(uint64(vcpus), 10),
	}
	if args.InitrdPath != "" {
		cmd = append(cmd, "--initramfs", args.InitrdPath)
	}
	// The rootfs block device comes from the unikernel's block CLI, the same
	// source Firecracker builds its `drives` array from -- args.BlockDevPath is
	// only wired on darwin. Without this a `mountRootfs=true` container boots
	// with root=/dev/vda and no vda, and vmi-init falls through to exec'ing the
	// entrypoint in the initramfs, which exits 127.
	//
	// hvi has exactly one --disk slot, so a container is only representable here
	// if it wants one block device; that one becomes the guest's /dev/vda, which
	// is what the Linux boot line names as root=. More than one has nowhere to
	// go, and we fail rather than drop the extras: a container that boots without
	// a volume it asked for is worse than one that refuses to start.
	blocks := make([]types.MonitorBlockArgs, 0, 1)
	for _, b := range ukernel.MonitorBlockCli() {
		if b.Path != "" {
			blocks = append(blocks, b)
		}
	}
	disk := args.BlockDevPath
	switch len(blocks) {
	case 0:
	case 1:
		disk = blocks[0].Path
	default:
		ids := make([]string, 0, len(blocks))
		for _, b := range blocks {
			ids = append(ids, b.ID)
		}
		return nil, fmt.Errorf("hvi supports a single block device, but %d were requested: %s",
			len(blocks), strings.Join(ids, ", "))
	}
	if disk != "" {
		cmd = append(cmd, "--disk", disk)
	}
	// hvi reaches the network through the same gateway socket QEMU uses; a bare
	// --net would give the guest hvi's built-in user-space stack instead, which
	// is not the container's network identity.
	if args.Net.TapDev != "" {
		cmd = append(cmd, "--net")
	}
	if args.ContainerID != "" {
		cmd = append(cmd, "--sandbox-id", args.ContainerID)
	}
	// Out-of-VMM introspection. urunc signals this by putting the control-socket
	// path in the monitor's environment (the Firecracker fork reads the variable
	// directly; hvi takes a flag), so translating it here keeps the opt-in in one
	// place and needs no change in the shim or the sidecar: both monitors end up
	// serving the same vmm-control protocol on the same path inside their own
	// mount namespace, which is where the sidecar looks for it.
	for _, e := range args.Environment {
		if v, ok := strings.CutPrefix(e, telemControlSockEnv+"="); ok && v != "" {
			cmd = append(cmd, "--introspect-sock", v)
			break
		}
	}
	// Self-introspection cadence, in seconds. Translated here for the same
	// reason as the socket above: hvi takes a flag where the Firecracker fork
	// reads the environment directly.
	for _, e := range args.Environment {
		if v, ok := strings.CutPrefix(e, introspectIntervalEnv+"="); ok && v != "" {
			cmd = append(cmd, "--introspect-interval", v)
			break
		}
	}
	if args.Command != "" {
		cmd = append(cmd, "--cmdline", args.Command)
	}
	return cmd, nil
}

// PreExec is a no-op: hvi needs no monitor-specific setup before exec.
func (h *Hvi) PreExec(_ types.ExecArgs) error {
	return nil
}
