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
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	QemuVmm    VmmType = "qemu"
	QemuBinary string  = "qemu-system-"
)

// Qemu is the single QEMU backend for every platform. The zero value targets
// Linux with KVM; the darwin constructor (NewQemuDarwin) flips the platform
// fields below to target Hypervisor.framework. Keeping one type — rather than
// a parallel QemuDarwin — means both platforms share the command-building
// skeleton and the VMM interface, with the genuinely platform-specific
// choices (accelerator, firmware path, network backend, and darwin's extra
// rootfs realization) localized to explicit branches.
type Qemu struct {
	binaryPath string
	binary     string
	vhost      bool

	// Platform configuration. Zero values reproduce the Linux/KVM backend
	// exactly, so a bare Qemu{} (as constructed by the Linux VMM factory and
	// the unit tests) is unchanged.
	darwin    bool   // Hypervisor.framework target (macOS) vs KVM (Linux)
	accel     string // accelerator flag; "" => "-enable-kvm"
	firmware  string // -L firmware path; "" => "/usr/share/qemu"
	qmpSocket string // QMP control socket; "" => no -qmp
}

func (q *Qemu) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

func (q *Qemu) Stop(pid int) error {
	return killProcess(pid)
}

func (q *Qemu) Ok() error {
	if q.binaryPath == "" {
		return fmt.Errorf("qemu binary path not set")
	}
	return nil
}

// UsesKVM reports whether the monitor uses KVM (Linux) as opposed to HVF.
func (q *Qemu) UsesKVM() bool {
	return !q.darwin
}

// SupportsSharedfs reports monitor support for shared-fs.
func (q *Qemu) SupportsSharedfs(_ string) bool {
	return true
}

func (q *Qemu) Path() string {
	return q.binaryPath
}

// accelArgs returns the accelerator arguments: "-enable-kvm" on Linux, or the
// configured flag (e.g. "-accel hvf") split into argv words on darwin.
func (q *Qemu) accelArgs() []string {
	if q.accel != "" {
		return strings.Fields(q.accel)
	}
	return []string{"-enable-kvm"}
}

func (q *Qemu) firmwarePath() string {
	if q.firmware != "" {
		return q.firmware
	}
	return "/usr/share/qemu"
}

// BuildExecCmd builds and validates the QEMU argument vector. The skeleton is
// shared across platforms; the branches guarded by q.darwin cover the
// Hypervisor.framework specifics (HVF accelerator, vmnet/stream networking,
// and the disk-image / directory-rootfs realization that on Linux is handled
// by the orchestration layer instead).
func (q *Qemu) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	qemuMem := BytesToStringMB(args.MemSizeB)
	exArgs := []string{
		q.binaryPath,
		"-m", qemuMem + "M",
		"-L", q.firmwarePath(), // Set the path for qemu bios/data
		"-cpu", "host", // Choose CPU
	}
	exArgs = append(exArgs, q.accelArgs()...) // KVM (Linux) or HVF (darwin) for CPU virt extensions
	exArgs = append(exArgs,
		"-display", "none", // Disable graphic output
		"-vga", "none",
		"-serial", "stdio",
		"-monitor", "null",
	)

	if args.VCPUs > 0 {
		exArgs = append(exArgs, "-smp", strconv.FormatUint(uint64(args.VCPUs), 10))
	}

	if args.Seccomp {
		// Enable Seccomp in QEMU. The sandbox options below:
		//   obsolete=deny          - Deny Obsolete system calls
		//   elevateprivileges=deny - Deny set*uid|gid system calls
		//   spawn=deny             - Deny *fork and execve
		//   resourcecontrol=deny   - Deny process affinity and scheduler priority
		exArgs = append(exArgs,
			"--sandbox", "on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny",
		)
	}

	// TODO: explore alternative implementations
	if runtime.GOARCH == "arm64" {
		exArgs = append(exArgs, "-M", "virt")
	}

	// Kernel vs disk image. On darwin a sibling disk.img (built from the
	// container rootfs) boots directly with EDK2 firmware; otherwise boot the
	// kernel. KernelPath (set on darwin) falls back to UnikernelPath so the
	// Linux path is unchanged.
	useDiskImage := false
	kernelPath := args.UnikernelPath
	if args.KernelPath != "" {
		kernelPath = args.KernelPath
	}
	if q.darwin {
		diskImagePath := filepath.Join(filepath.Dir(args.UnikernelPath), "disk.img")
		if _, err := os.Stat(diskImagePath); err == nil {
			exArgs = append(exArgs,
				"-drive", "format=raw,file="+diskImagePath+",if=virtio",
				"-bios", "/opt/homebrew/share/qemu/edk2-aarch64-code.fd",
			)
			useDiskImage = true
		}
	}
	if !useDiskImage {
		exArgs = append(exArgs, "-kernel", kernelPath)
	}

	// Networking: platform-specific backend.
	exArgs = append(exArgs, q.netCli(args, ukernel)...)

	// Block devices (shared).
	blockArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range blockArgs {
		if blockArg.ID != "" && blockArg.Path != "" {
			devArg := fmt.Sprintf("virtio-blk-pci,serial=%s,drive=%s,scsi=off", blockArg.ID, blockArg.ID)
			drvArg := fmt.Sprintf("format=raw,if=none,id=%s,file=%s", blockArg.ID, blockArg.Path)
			exArgs = append(exArgs, "-device", devArg, "-drive", drvArg)
		} else if len(blockArg.ExactArgs) > 0 {
			exArgs = append(exArgs, blockArg.ExactArgs...)
		}
	}

	if args.InitrdPath != "" {
		exArgs = append(exArgs, "-initrd", args.InitrdPath)
	}

	// Shared filesystem (Linux 9pfs/virtiofs backends).
	switch args.Sharedfs.Type {
	case "9pfs":
		fsdevArg := fmt.Sprintf("local,id=rootfs9p,security_model=none,path=%s", args.Sharedfs.Path)
		exArgs = append(exArgs, "-fsdev", fsdevArg, "-device", "virtio-9p-pci,fsdev=rootfs9p,mount_tag=fs0")
	case "virtiofs":
		objArg := fmt.Sprintf("memory-backend-file,id=mem,size=%sM,mem-path=/tmp,share=on", qemuMem)
		exArgs = append(exArgs,
			"-object", objArg,
			"-numa", "node,memdev=mem",
			"-chardev", "socket,id=char0,path=/tmp/vhostqemu",
			"-device", "vhost-user-fs-pci,queue-size=1024,chardev=char0,tag=fs0",
		)
	default:
		// Nothing to add
	}

	// Darwin realizes the container rootfs as a 9p share (directory) or
	// initrd (file), plus any extra tagged shares. On Linux this is the
	// orchestration layer's job, so it stays behind q.darwin.
	if q.darwin {
		exArgs = append(exArgs, q.darwinRootfsArgs(args)...)
		for _, dir := range args.SharedDirs {
			exArgs = append(exArgs,
				"-fsdev", "local,id="+dir.Tag+",security_model=none,path="+dir.Path,
				"-device", "virtio-9p-pci,fsdev="+dir.Tag+",mount_tag="+dir.Tag,
			)
		}
	}

	extraMonArgs := ukernel.MonitorCli()
	if extraInitrd := extraMonArgs.ExtraInitrd; extraInitrd != "" {
		// The unikernel returns the guest-absolute path of the urunit config
		// blob (e.g. "/urunit.conf"). On Linux the reexec'd runtime lives in
		// the container's mount namespace so that path resolves against the
		// rootfs directly. On darwin there is no such namespace, so resolve it
		// against the host rootfs directory where setupUrunitConfig wrote it.
		if q.darwin && filepath.IsAbs(extraInitrd) && args.Sharedfs.Path != "" {
			extraInitrd = filepath.Join(args.Sharedfs.Path, extraInitrd)
		}
		exArgs = append(exArgs, "-initrd", extraInitrd)
	}
	exArgs = append(exArgs, extraMonArgs.OtherArgs...)

	if args.VAccelType == "vsock" {
		vsockArg := fmt.Sprintf("vhost-vsock-pci,id=vhost-vsock-pci0,guest-cid=%d", args.VSockDevID)
		exArgs = append(exArgs, "-device", vsockArg)
	}

	if q.qmpSocket != "" {
		exArgs = append(exArgs, "-qmp", "unix:"+q.qmpSocket+",server,nowait")
	}

	// A disk image carries its own boot config; only pass -append with a kernel.
	if !useDiskImage {
		exArgs = append(exArgs, "-append", args.Command)
	}
	return exArgs, nil
}

// netCli returns the platform network arguments. The Linux path (tap + vhost)
// is the zero-value default; darwin uses a user-mode-gateway stream netdev or
// vmnet-shared. A unikernel-provided MonitorNetCli overrides both.
func (q *Qemu) netCli(args types.ExecArgs, ukernel types.Unikernel) []string {
	if q.darwin {
		if args.Net.UnixSocket != "" {
			return []string{
				"-netdev", "stream,id=net0,addr.type=unix,addr.path=" + args.Net.UnixSocket,
				"-device", "virtio-net-pci,netdev=net0,mac=" + args.Net.MAC,
			}
		}
		if args.Net.TapDev != "" {
			if netcli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC); len(netcli) > 0 {
				return netcli
			}
			return []string{
				"-netdev", "vmnet-shared,id=net0",
				"-device", "virtio-net-pci,netdev=net0,mac=" + args.Net.MAC,
			}
		}
		return []string{"-nic", "none"}
	}

	if args.Net.TapDev != "" {
		netcli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
		if len(netcli) > 0 {
			return netcli
		}
		netdevArg := fmt.Sprintf("tap,id=net0,script=no,downscript=no,ifname=%s", args.Net.TapDev)
		if q.vhost {
			netdevArg += ",vhost=on"
		}
		devType := "virtio-net-pci"
		if runtime.GOARCH == "arm64" {
			devType = "virtio-net-device"
		}
		netCliDev := fmt.Sprintf("%s,netdev=net0,host_mtu=%d,mac=%s", devType, args.Net.MTU, args.Net.MAC)
		return []string{"-netdev", netdevArg, "-device", netCliDev}
	}
	return []string{"-nic", "none"}
}

// darwinRootfsArgs shares the container rootfs directory over 9p (mount tag
// "rootfs"), or attaches a rootfs file as initrd. Empty when a block device
// already provides the root.
func (q *Qemu) darwinRootfsArgs(args types.ExecArgs) []string {
	if args.BlockDevPath != "" {
		return []string{"-drive", "format=raw,file=" + args.BlockDevPath + ",if=virtio"}
	}
	if args.RootfsPath == "" {
		return nil
	}
	if info, err := os.Stat(args.RootfsPath); err == nil && info.IsDir() {
		return []string{
			"-fsdev", "local,id=rootdev,security_model=none,path=" + args.RootfsPath,
			"-device", "virtio-9p-pci,fsdev=rootdev,mount_tag=rootfs",
		}
	}
	return []string{"-initrd", args.RootfsPath}
}

// PreExec performs pre-execution setup. On darwin it clears any stale QMP
// socket; on Linux QEMU needs nothing.
func (q *Qemu) PreExec(_ types.ExecArgs) error {
	if q.qmpSocket != "" {
		_ = os.Remove(q.qmpSocket)
	}
	return nil
}

// GetQMPSocket returns the QMP control socket path (darwin lifecycle control).
func (q *Qemu) GetQMPSocket() string { return q.qmpSocket }

// SetQMPSocket sets the QMP control socket path.
func (q *Qemu) SetQMPSocket(path string) { q.qmpSocket = path }

// SetBinaryPath overrides the QEMU binary path.
func (q *Qemu) SetBinaryPath(path string) { q.binaryPath = path }
