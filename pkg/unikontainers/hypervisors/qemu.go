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
	"runtime"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const (
	QemuVmm    VmmType = "qemu"
	QemuBinary string  = "qemu-system-"
)

type Qemu struct {
	binaryPath string
	binary     string
	vhost      bool
}

func (q *Qemu) Stop(pid int) error {
	return killProcess(pid)
}

func (q *Qemu) Ok() error {
	return nil
}

// UsesKVM returns a bool value depending on if the monitor uses KVM
func (q *Qemu) UsesKVM() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (q *Qemu) SupportsSharedfs(_ string) bool {
	return true
}

func (q *Qemu) Path() string {
	return q.binaryPath
}

func (q *Qemu) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	qemuMem := BytesToStringMB(args.MemSizeB)

	exArgs := []string{q.binaryPath}

	exArgs = append(exArgs, "-m", qemuMem+"M")
	exArgs = append(exArgs, "-L", "/usr/share/qemu")
	exArgs = append(exArgs, "-cpu", "host")
	exArgs = append(exArgs, "-enable-kvm")
	exArgs = append(exArgs, "-display", "none", "-vga", "none", "-serial", "stdio", "-monitor", "null")

	if args.VCPUs > 0 {
		exArgs = append(exArgs, "-smp", fmt.Sprintf("%d", args.VCPUs))
	}

	if args.Seccomp {
		exArgs = append(exArgs, "--sandbox", "on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny")
	}

	if runtime.GOARCH == "arm64" {
		exArgs = append(exArgs, "-M", "virt")
	}

	exArgs = append(exArgs, "-kernel", args.UnikernelPath)

	if args.Net.TapDev != "" {
		netcli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
		if netcli == "" {
			netcliArgs := []string{
				"-netdev", fmt.Sprintf("tap,id=net0,script=no,downscript=no,ifname=%s", args.Net.TapDev),
			}
			if q.vhost {
				netcliArgs[0] += ",vhost=on"
			}
			netcliArgs = append(netcliArgs, fmt.Sprintf("%s,host_mtu=%d,mac=%s", getVirtioNetArg(), args.Net.MTU, args.Net.MAC))
			exArgs = append(exArgs, netcliArgs...)
		} else {
			exArgs = append(exArgs, strings.Fields(netcli)...)
		}
	} else {
		exArgs = append(exArgs, "-nic", "none")
	}

	blockArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range blockArgs {
		blockCli := blockArg.ExactArgs
		if blockCli == "" && blockArg.ID != "" && blockArg.Path != "" {
			exArgs = append(exArgs,
				"-device", fmt.Sprintf("virtio-blk-pci,serial=%s,drive=%s,scsi=off", blockArg.ID, blockArg.ID),
				"-drive", fmt.Sprintf("format=raw,if=none,id=%s,file=%s", blockArg.ID, blockArg.Path),
			)
		} else if blockCli != "" {
			exArgs = append(exArgs, strings.Fields(blockCli)...)
		}
	}

	if args.InitrdPath != "" {
		exArgs = append(exArgs, "-initrd", args.InitrdPath)
	}

	switch args.Sharedfs.Type {
	case "9pfs":
		exArgs = append(exArgs,
			"-fsdev", fmt.Sprintf("local,id=rootfs9p,security_model=none,path=%s", args.Sharedfs.Path),
			"-device", "virtio-9p-pci,fsdev=rootfs9p,mount_tag=fs0",
		)
	case "virtiofs":
		exArgs = append(exArgs,
			"-object", fmt.Sprintf("memory-backend-file,id=mem,size=%sM,mem-path=/tmp,share=on", qemuMem),
			"-numa", "node,memdev=mem",
			"-chardev", "socket,id=char0,path=/tmp/vhostqemu",
			"-device", "vhost-user-fs-pci,queue-size=1024,chardev=char0,tag=fs0",
		)
	default:
		// Nothing to add
	}

	extraMonArgs := ukernel.MonitorCli()
	if extraMonArgs.ExtraInitrd != "" {
		exArgs = append(exArgs, "-initrd", extraMonArgs.ExtraInitrd)
	}

	if extraMonArgs.OtherArgs != "" {
		exArgs = append(exArgs, strings.Fields(extraMonArgs.OtherArgs)...)
	}

	if args.VAccelType == "vsock" {
		exArgs = append(exArgs,
			"-device", fmt.Sprintf("vhost-vsock-pci,id=vhost-vsock-pci0,guest-cid=%d", args.VSockDevID),
		)
	}

	exArgs = append(exArgs, "-append", args.Command)
	return exArgs, nil
}

// PreExec performs pre-execution setup. QEMU has no special pre-exec requirements.
func (q *Qemu) PreExec(_ types.ExecArgs) error {
	return nil
}

func getVirtioNetArg() string {
	devType := "virtio-net-pci"
	if runtime.GOARCH == "arm64" {
		devType = "virtio-net-device"
	}
	return "-device " + devType + ",netdev=net0"
}
