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

//go:build darwin

package hypervisors

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

// HviDarwin runs an arm64 Linux guest with Hypervisor.framework and hvi's
// in-process virtio devices. Generic container boot exports the unpacked OCI
// directory read-only through hvi's virtio-fs backend; the initrd supplies the
// writable overlay. Additional tagged directories keep their requested
// read-only/read-write mode.
//
// Every export is one --fs, in the comma-separated form hvi took from Cloud
// Hypervisor: tag=<tag>,path=<dir>[,readonly=on][,uid=<n>,gid=<n>]. The
// older --share-ro/--share-rw pairs and the process-wide --fs-uid/--fs-gid
// still parse, but hvi prints a note for each, and uid= and gid= let every
// export carry its own owner.
type HviDarwin struct {
	binaryPath string
}

func NewHviDarwin(binaryPath string) *HviDarwin {
	return &HviDarwin{binaryPath: binaryPath}
}

func (h *HviDarwin) BuildExecCmd(args types.ExecArgs, _ types.Unikernel) ([]string, error) {
	kernel := args.KernelPath
	if kernel == "" {
		kernel = args.UnikernelPath
	}
	if kernel == "" {
		return nil, fmt.Errorf("hvi: no kernel image to boot")
	}
	mem := DefaultMemory
	if args.MemSizeB != 0 {
		mem = bytesToMiB(args.MemSizeB)
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
	if args.Command != "" {
		cmd = append(cmd, "--cmdline", args.Command)
	}
	if args.BlockDevPath != "" {
		cmd = append(cmd, "--disk", args.BlockDevPath)
	}
	seenTags := make(map[string]bool)
	appendShare := func(path, tag string, readOnly bool) error {
		if path == "" || tag == "" {
			return fmt.Errorf("hvi virtio-fs exports require a path and tag")
		}
		// The option string is split on commas, with no escape, so a comma in
		// either value would be read as the start of the next option.
		if strings.ContainsRune(path, ',') || strings.ContainsRune(tag, ',') {
			return fmt.Errorf("hvi virtio-fs export %q at %q: a comma cannot be passed in --fs", tag, path)
		}
		if seenTags[tag] {
			return fmt.Errorf("duplicate hvi virtio-fs tag %q", tag)
		}
		seenTags[tag] = true
		export := "tag=" + tag + ",path=" + path
		if readOnly {
			export += ",readonly=on"
		}
		// Tell the file server who the workload is, so the export comes back
		// owned by it. Only when it is not root: root is hvi's default.
		if args.GuestUID != 0 || args.GuestGID != 0 {
			export += ",uid=" + strconv.FormatUint(uint64(args.GuestUID), 10) +
				",gid=" + strconv.FormatUint(uint64(args.GuestGID), 10)
		}
		cmd = append(cmd, "--fs", export)
		return nil
	}
	if args.Sharedfs.Path != "" {
		tag := args.Sharedfs.Tag
		if tag == "" {
			tag = "rootfs"
		}
		if err := appendShare(args.Sharedfs.Path, tag, args.Sharedfs.ReadOnly); err != nil {
			return nil, err
		}
	}
	for _, dir := range args.SharedDirs {
		if err := appendShare(dir.Path, dir.Tag, dir.ReadOnly); err != nil {
			return nil, err
		}
	}
	if args.Net.UnixSocket != "" {
		cmd = append(cmd, "--net-gateway", args.Net.UnixSocket)
	} else if args.Net.TapDev != "" {
		cmd = append(cmd, "--net")
	}
	// The guest's MAC is how the gateway tells one sandbox from another: it
	// keys its DHCP leases by it. Without this every guest presents the VMM's
	// built-in default, so a second sandbox takes the first one's lease.
	if args.Net.MAC != "" {
		cmd = append(cmd, "--net-mac", args.Net.MAC)
	}
	if args.AgentSockPath != "" {
		cmd = append(cmd, "--agent-sock", args.AgentSockPath)
	}
	if args.ContainerID != "" {
		cmd = append(cmd, "--sandbox-id", args.ContainerID)
	}
	return cmd, nil
}

func (h *HviDarwin) PreExec(_ types.ExecArgs) error { return nil }

func (h *HviDarwin) Signal(pid int, signal unix.Signal) error {
	return signalProcess(pid, signal)
}

func (h *HviDarwin) Stop(pid int) error { return killProcess(pid) }

func (h *HviDarwin) Path() string { return h.binaryPath }

func (h *HviDarwin) UsesKVM() bool { return false }

func (h *HviDarwin) SupportsSharedfs(fsType string) bool { return fsType == "virtiofs" }

// UsesTUNTAP returns false: macOS has no tap, and hvi networks the guest
// through vmnet or hull's gateway socket.
func (h *HviDarwin) UsesTUNTAP() bool { return false }

// SupportsControlSocket returns false: hvi on darwin has no control API.
func (h *HviDarwin) SupportsControlSocket() bool { return false }

func (h *HviDarwin) Ok() error {
	info, err := os.Stat(h.binaryPath)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("hvi not found or not executable at %s", h.binaryPath)
	}
	return nil
}
