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
	CloudHypervisorVmm    VmmType = "cloud-hypervisor"
	CloudHypervisorBinary string  = "cloud-hypervisor"
)

type CloudHypervisor struct {
	binaryPath string
	binary     string
}

func (ch *CloudHypervisor) Signal(pid int, signal unix.Signal) error {
	return signalProcess(pid, signal)
}

func (ch *CloudHypervisor) Stop(pid int) error {
	return killProcess(pid)
}

func (ch *CloudHypervisor) Ok() error {
	return nil
}

// UsesKVM returns true as Cloud Hypervisor is a KVM-based VMM
func (ch *CloudHypervisor) UsesKVM() bool {
	return true
}

// UsesTUNTAP reports that the guest gets its network through a tap device.
func (ch *CloudHypervisor) UsesTUNTAP() bool {
	return true
}

// SupportsControlSocket reports that Cloud Hypervisor exposes a control socket
// (its REST API socket).
func (ch *CloudHypervisor) SupportsControlSocket() bool {
	return true
}

// SupportsSharedfs returns true as Cloud Hypervisor supports virtiofs
func (ch *CloudHypervisor) SupportsSharedfs(fsType string) bool {
	switch fsType {
	case "virtio":
		return true
	default:
		return false
	}
}

func (ch *CloudHypervisor) Path() string {
	return ch.binaryPath
}

// BuildExecCmd builds and validates the Cloud Hypervisor command arguments without executing.
func (ch *CloudHypervisor) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	chMem := BytesToStringMB(args.MemSizeB)

	// Start building the command
	exArgs := []string{ch.binaryPath}

	// Expose the REST API socket only when socket_path is set, so the runtime
	// can talk to Cloud Hypervisor after boot.
	if args.SocketPath != "" {
		exArgs = append(exArgs, "--api-socket", "path="+args.SocketPath)
	}

	// Memory configuration. A vhost-user daemon maps the guest memory, so
	// any virtio-fs export needs it shared.
	if args.Sharedfs.Type == "virtiofs" || len(args.VirtiofsExports) > 0 {
		memArg := "size=" + chMem + "M,shared=on"
		exArgs = append(exArgs, "--memory", memArg)
	} else {
		memArg := "size=" + chMem + "M"
		exArgs = append(exArgs, "--memory", memArg)
	}

	// CPU configuration
	if args.VCPUs > 0 {
		cpuArg := "boot=" + strconv.FormatUint(uint64(args.VCPUs), 10)
		exArgs = append(exArgs, "--cpus", cpuArg)
	}

	// Kernel path
	exArgs = append(exArgs, "--kernel", args.UnikernelPath)

	// A guest asking for hvc0 gets a virtio console and no serial port, as
	// with qemu. Any other guest keeps the serial port on the tty.
	if strings.Contains(args.Command, "console=hvc0") {
		exArgs = append(exArgs, "--console", "tty", "--serial", "off")
	} else {
		exArgs = append(exArgs, "--console", "off", "--serial", "tty")
	}

	// Seccomp configuration
	if args.Seccomp {
		exArgs = append(exArgs, "--seccomp", "true")
	} else {
		exArgs = append(exArgs, "--seccomp", "false")
	}

	// Network configuration
	if args.Net.TapDev != "" {
		netCli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
		if len(netCli) == 0 {
			// No mtu= here. urunc sets the tap's MTU when it creates the
			// device, from the interface it redirects, so naming it again
			// only makes the monitor repeat the work. It cannot: the monitor
			// runs as the container's user and SIOCSIFMTU needs CAP_NET_ADMIN,
			// so an image running as anyone but root fails to boot here.
			netArg := fmt.Sprintf("tap=%s,mac=%s", args.Net.TapDev, args.Net.MAC)
			exArgs = append(exArgs, "--net", netArg)
		} else {
			exArgs = append(exArgs, netCli...)
		}
	}

	// Block device configuration
	blockArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range blockArgs {
		if len(blockArg.ExactArgs) > 0 {
			exArgs = append(exArgs, blockArg.ExactArgs...)
		} else if blockArg.Path != "" {
			// The option string is split on commas, with no escape.
			if strings.ContainsRune(blockArg.Path, ',') {
				return nil, fmt.Errorf("cloud-hypervisor: block device %q: a comma cannot be passed in --disk", blockArg.Path)
			}
			diskArg := "path=" + blockArg.Path
			if blockArg.ID != "" {
				diskArg += ",id=" + blockArg.ID
			}
			if blockArg.ReadOnly {
				diskArg += ",readonly=on"
			}
			if blockArg.Serial != "" {
				diskArg += ",serial=" + blockArg.Serial
			}
			// cloud-hypervisor v54.0 removed image type autodetection and
			// refuses a --disk that does not name one. What urunc hands it
			// is a block device or a raw file, never a qcow2, so say so,
			// the way the qemu path says format=raw. The parameter exists
			// since v51.0.
			diskArg += ",image_type=raw"
			exArgs = append(exArgs, "--disk", diskArg)
		}
	}

	// Initrd configuration
	if args.InitrdPath != "" {
		exArgs = append(exArgs, "--initramfs", args.InitrdPath)
	}

	// Check for extra initrd from unikernel monitor args
	extraMonArgs := ukernel.MonitorCli()
	if extraMonArgs.ExtraInitrd != "" {
		exArgs = append(exArgs, "--initramfs", extraMonArgs.ExtraInitrd)
	}

	switch args.Sharedfs.Type {
	case "virtiofs":
		exArgs = append(exArgs, "--fs", "tag=fs0,socket=/tmp/vhostqemu")
	default:
		// No shared filesystem
	}
	for _, e := range args.VirtiofsExports {
		spec, err := virtiofsExportSpec("cloud-hypervisor", e)
		if err != nil {
			return nil, err
		}
		exArgs = append(exArgs, "--fs", spec)
	}

	// Cloud Hypervisor allows a single vsock device, a host unix socket that
	// multiplexes guest ports. vAccel (guest-initiated) and the exec agent
	// (host-initiated) share it; both derive the same guest CID from the id.
	switch {
	case args.VAccelType == "vsock":
		vsockArg := fmt.Sprintf("cid=%d,socket=%s/vaccel.sock", args.VSockDevID, args.VSockDevPath)
		exArgs = append(exArgs, "--vsock", vsockArg)
	case args.AgentVsockCID != 0:
		// Container boot: expose the vsock so urunc exec can reach the in-guest
		// agent by connecting to this socket and asking for the agent port.
		vsockArg := fmt.Sprintf("cid=%d,socket=%s", args.AgentVsockCID, args.AgentVsockUDS)
		exArgs = append(exArgs, "--vsock", vsockArg)
	}

	if len(extraMonArgs.OtherArgs) > 0 {
		exArgs = append(exArgs, extraMonArgs.OtherArgs...)
	}

	// Add the command line arguments for the kernel
	exArgs = append(exArgs, "--cmdline", args.Command)

	vmmLog.WithField("cloud-hypervisor command", exArgs).Debug("Ready to execve cloud-hypervisor")

	return exArgs, nil
}

// PreExec performs pre-execution setup. Cloud Hypervisor has no special pre-exec requirements.
func (ch *CloudHypervisor) PreExec(_ types.ExecArgs) error {
	return nil
}
