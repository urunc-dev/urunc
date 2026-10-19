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
	"slices"
	"strconv"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	QemuVmm    VmmType = "qemu"
	QemuBinary string  = "qemu-system-"
)

// Qemu is the QEMU backend on every platform. The zero value targets Linux
// with KVM. NewQemuDarwin sets the platform fields below to target
// Hypervisor.framework on macOS, so both platforms share one command builder
// and the platform choices (accelerator, firmware path, network backend and
// the darwin rootfs) sit in explicit branches.
type Qemu struct {
	binaryPath string
	binary     string
	vhost      bool

	// Platform configuration. The zero values reproduce the Linux/KVM
	// command line exactly.
	darwin    bool   // Hypervisor.framework (macOS) instead of KVM (Linux)
	accel     string // accelerator flags; "" means -enable-kvm
	firmware  string // -L firmware path; "" means /usr/share/qemu
	qmpSocket string // QMP socket set by a darwin caller; "" means none
}

func (q *Qemu) Signal(pid int, signal unix.Signal) error {
	return signalProcess(pid, signal)
}

func (q *Qemu) Stop(pid int) error {
	return killProcess(pid)
}

func (q *Qemu) Ok() error {
	return nil
}

// UsesKVM reports whether the monitor uses KVM (Linux) rather than HVF.
func (q *Qemu) UsesKVM() bool {
	return !q.darwin
}

// UsesTUNTAP reports that the guest gets its network through a tap device.
func (q *Qemu) UsesTUNTAP() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (q *Qemu) SupportsSharedfs(_ string) bool {
	return true
}

// SupportsControlSocket reports that QEMU exposes a control socket (QMP).
func (q *Qemu) SupportsControlSocket() bool {
	return true
}

func (q *Qemu) Path() string {
	return q.binaryPath
}

// accelArgs returns the accelerator arguments: -enable-kvm on Linux, or the
// configured flags (e.g. "-accel hvf") split into argv words on darwin.
func (q *Qemu) accelArgs() []string {
	if q.accel != "" {
		return strings.Fields(q.accel)
	}
	return []string{"-enable-kvm"}
}

// firmwarePath returns the directory QEMU loads its bios and data from.
func (q *Qemu) firmwarePath() string {
	if q.firmware != "" {
		return q.firmware
	}
	return "/usr/share/qemu"
}

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
		"-monitor", "null",
	)
	// Expose the QMP socket only when socket_path is set. server=on,wait=off lets
	// QEMU boot without waiting for a client.
	if args.SocketPath != "" {
		exArgs = append(exArgs, "-qmp", "unix:"+args.SocketPath+",server=on,wait=off")
	}
	// The guest boot cmdline selects the console device: a guest asking
	// for hvc0 gets a virtio console, where console I/O rides virtqueues
	// instead of trapping to the VMM on every UART register access. Any
	// other guest keeps the emulated UART on stdio. signal=off leaves
	// terminal signals (e.g. Ctrl-C) to the guest instead of QEMU.
	if strings.Contains(args.Command, "console=hvc0") {
		exArgs = append(exArgs,
			"-chardev", "stdio,id=urunc-console,signal=off",
			"-device", "virtio-serial-pci",
			"-device", "virtconsole,chardev=urunc-console",
		)
	} else {
		exArgs = append(exArgs, "-serial", "stdio")
	}

	// The exec agent transport on hosts without vhost-vsock (macOS): a
	// virtio-serial port bridged to a host unix socket. The guest agent
	// finds the port by its name in sysfs, so the name must match the
	// agent's virtio port name. One virtio-serial-pci serves both the
	// console and the agent port.
	if args.AgentSockPath != "" {
		if !slices.Contains(exArgs, "virtio-serial-pci") {
			exArgs = append(exArgs, "-device", "virtio-serial-pci")
		}
		exArgs = append(exArgs,
			"-chardev", "socket,id=uragent0,path="+args.AgentSockPath+",server=on,wait=off",
			"-device", "virtserialport,chardev=uragent0,name=io.urunc.agent.0",
		)
	}

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

	// Kernel or disk image. On darwin a disk.img next to the unikernel
	// boots directly with EDK2 firmware; otherwise QEMU boots the kernel.
	// KernelPath (set on darwin) falls back to UnikernelPath, so Linux
	// boots UnikernelPath as before.
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

	exArgs = append(exArgs, q.netCli(args, ukernel)...)

	exArgs = append(exArgs, qemuBlockArgs(ukernel.MonitorBlockCli())...)

	if args.InitrdPath != "" {
		exArgs = append(exArgs, "-initrd", args.InitrdPath)
	}

	fsArgs, err := q.sharedfsArgs(args, ukernel, qemuMem)
	if err != nil {
		return nil, err
	}
	exArgs = append(exArgs, fsArgs...)

	// On darwin QEMU also realizes the container rootfs, as a raw disk, a
	// 9p share (directory) or an initrd (file), plus any extra tagged
	// shares. On Linux the rootfs is set up before QEMU starts.
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
		// The unikernel returns the guest-absolute path of the urunit
		// config (e.g. /urunit.conf). On Linux QEMU runs inside the
		// container's mount namespace, where that path resolves against
		// the rootfs. darwin has no such namespace, so resolve it against
		// the host rootfs directory instead.
		if q.darwin && filepath.IsAbs(extraInitrd) && args.Sharedfs.Path != "" {
			extraInitrd = filepath.Join(args.Sharedfs.Path, extraInitrd)
		}
		exArgs = append(exArgs, "-initrd", extraInitrd)
	}
	exArgs = append(exArgs, extraMonArgs.OtherArgs...)

	// A single vhost-vsock device carries the guest CID for both the exec agent
	// and vAccel: both derive it from the container id, so the values agree and
	// the device is emitted once when either transport needs it.
	vsockCID := args.AgentVsockCID
	if args.VAccelType == "vsock" {
		vsockCID = args.VSockDevID
	}
	if vsockCID != 0 {
		vsockArg := fmt.Sprintf("vhost-vsock-pci,id=vhost-vsock-pci0,guest-cid=%d", vsockCID)
		exArgs = append(exArgs, "-device", vsockArg)
	}

	// A darwin caller that sets the QMP socket on the monitor gets it here,
	// unless ExecArgs.SocketPath already added one: QEMU takes one -qmp.
	if args.SocketPath == "" && q.qmpSocket != "" {
		exArgs = append(exArgs, "-qmp", "unix:"+q.qmpSocket+",server,nowait")
	}

	// A disk image carries its own boot configuration, so -append only
	// goes with a kernel.
	if !useDiskImage {
		exArgs = append(exArgs, "-append", args.Command)
	}
	return exArgs, nil
}

// qemuBlockArgs returns the arguments of the guest's block devices: a
// virtio-blk device with the block ID as its serial, read-only when asked,
// or the exact arguments the guest supplies.
func qemuBlockArgs(blockArgs []types.MonitorBlockArgs) []string {
	var args []string
	for _, blockArg := range blockArgs {
		if blockArg.ID != "" && blockArg.Path != "" {
			devArg := fmt.Sprintf("virtio-blk-pci,serial=%s,drive=%s,scsi=off", blockArg.ID, blockArg.ID)
			drvArg := fmt.Sprintf("format=raw,if=none,id=%s,file=%s", blockArg.ID, blockArg.Path)
			if blockArg.ReadOnly {
				drvArg += ",readonly=on"
			}
			args = append(args, "-device", devArg, "-drive", drvArg)
		} else if len(blockArg.ExactArgs) > 0 {
			args = append(args, blockArg.ExactArgs...)
		}
	}
	return args
}

// sharedfsArgs returns the arguments of the rootfs share and of the
// vhost-user virtio-fs exports. A vhost-user daemon maps the guest memory, so
// any virtio-fs export needs it in a shared file. The darwin path has no
// vhost-user exports.
func (q *Qemu) sharedfsArgs(args types.ExecArgs, ukernel types.Unikernel, qemuMem string) ([]string, error) {
	var fsArgs []string
	var exports []types.VirtiofsExport
	if !q.darwin {
		exports = args.VirtiofsExports
	}
	if args.Sharedfs.Type == "virtiofs" || len(exports) > 0 {
		objArg := fmt.Sprintf("memory-backend-file,id=mem,size=%sM,mem-path=/tmp,share=on", qemuMem)
		fsArgs = append(fsArgs, "-object", objArg, "-numa", "node,memdev=mem")
	}
	switch args.Sharedfs.Type {
	case "9pfs":
		// The 9p transport depends on the guest, so let the guest supply
		// the cli options; nil means no support for 9pfs
		sharedFSOption := ukernel.MonitorSharedfsCli(args.Sharedfs.Type, args.Sharedfs.Path)
		fsArgs = append(fsArgs, sharedFSOption...)
	case "virtiofs":
		fsArgs = append(fsArgs,
			"-chardev", "socket,id=char0,path=/tmp/vhostqemu",
			"-device", "vhost-user-fs-pci,queue-size=1024,chardev=char0,tag=fs0",
		)
	default:
		// Nothing to add
	}
	for i, e := range exports {
		if e.Tag == "" || e.Socket == "" || strings.ContainsAny(e.Tag+e.Socket, ",=") {
			return nil, fmt.Errorf("qemu: virtio-fs export %+v needs a tag and a socket with no comma or =", e)
		}
		chardev := "charfs" + strconv.Itoa(i)
		fsArgs = append(fsArgs,
			"-chardev", "socket,id="+chardev+",path="+e.Socket,
			"-device", "vhost-user-fs-pci,queue-size=1024,chardev="+chardev+",tag="+e.Tag,
		)
	}
	return fsArgs, nil
}

// netCli returns the network arguments. On Linux that is a tap device, with
// vhost when configured. On darwin it is a stream netdev to a user-mode
// gateway socket, or vmnet-shared. A unikernel's own MonitorNetCli wins over
// the tap and vmnet defaults.
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

	if args.Net.TapDev == "" {
		return []string{"-nic", "none"}
	}
	if netcli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC); len(netcli) > 0 {
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

// darwinRootfsArgs attaches BlockDevPath as a raw virtio disk, or shares a
// RootfsPath directory over 9p (mount tag rootfs), or attaches a RootfsPath
// file as an initrd.
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

// PreExec removes a stale QMP socket left by an earlier darwin run, so QEMU
// can bind it again. Linux needs nothing here.
func (q *Qemu) PreExec(_ types.ExecArgs) error {
	if q.qmpSocket != "" {
		if err := os.Remove(q.qmpSocket); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale QMP socket %s: %w", q.qmpSocket, err)
		}
	}
	return nil
}

// GetQMPSocket returns the QMP socket a darwin caller set.
func (q *Qemu) GetQMPSocket() string { return q.qmpSocket }

// SetQMPSocket sets the QMP socket. ExecArgs.SocketPath takes precedence.
func (q *Qemu) SetQMPSocket(path string) { q.qmpSocket = path }

// SetBinaryPath overrides the QEMU binary path.
func (q *Qemu) SetBinaryPath(path string) { q.binaryPath = path }
