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
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// fakeUnikernel is a minimal stub of types.Unikernel used only to drive
// Qemu.BuildExecCmd. The four Monitor* methods are the ones the function
// consults; the rest return zero values.
type fakeUnikernel struct {
	netCli      []string
	blockCli    []types.MonitorBlockArgs
	sharedfsCli []string
	monitorCli  types.MonitorCliArgs
}

func (f *fakeUnikernel) Init(types.UnikernelParams) error           { return nil }
func (f *fakeUnikernel) CommandString() (string, error)             { return "", nil }
func (f *fakeUnikernel) SupportsBlock() bool                        { return true }
func (f *fakeUnikernel) SupportsFS(string) bool                     { return true }
func (f *fakeUnikernel) MonitorNetCli(string, string) []string      { return f.netCli }
func (f *fakeUnikernel) MonitorBlockCli() []types.MonitorBlockArgs  { return f.blockCli }
func (f *fakeUnikernel) MonitorSharedfsCli(string, string) []string { return f.sharedfsCli }
func (f *fakeUnikernel) MonitorCli() types.MonitorCliArgs           { return f.monitorCli }

const (
	testQemuBinary = "/usr/bin/qemu-system-x86_64"
	testKernelPath = "/rootfs/unikernel.bin"
	testCommand    = "init=/bin/sh"
)

func TestQemuBuildExecCmd(t *testing.T) {
	t.Parallel()

	archFlag := "-M virt"
	archShouldBePresent := runtime.GOARCH == "arm64"

	tests := []struct {
		name           string
		vhost          bool
		args           types.ExecArgs
		unikernel      types.Unikernel
		mustContain    []string
		mustNotContain []string
	}{
		{
			// Baseline: zero-value args (except the required UnikernelPath /
			// Command) exercise every default branch in BuildExecCmd at once —
			// static flags, default memory, kernel path, omitted -smp, omitted
			// --sandbox, -nic none, no -initrd, no shared-fs, no block devices,
			// no vsock.
			name: "defaults render baseline qemu command",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"-L /usr/share/qemu",
				"-cpu host",
				"-enable-kvm",
				"-display none",
				"-vga none",
				"-serial stdio",
				"-monitor null",
				"-m 256M",
				"-kernel " + testKernelPath,
				"-nic none",
			},
			mustNotContain: []string{
				"-smp",
				"--sandbox",
				"-initrd",
				"-fsdev",
				"vhost-user-fs-pci",
				"virtio-blk-pci",
				"vhost-vsock-pci",
				"-qmp",
			},
		},
		{
			name: "configured SocketPath renders -qmp on that path",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				SocketPath:    "/run/urunc/q.sock",
			},
			unikernel:   &fakeUnikernel{},
			mustContain: []string{"-qmp unix:/run/urunc/q.sock,server=on,wait=off"},
		},
		{
			name: "unset SocketPath omits -qmp",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				ContainerID:   "abc123",
			},
			unikernel:      &fakeUnikernel{},
			mustNotContain: []string{"-qmp"},
		},
		{
			// A guest cmdline that selects hvc0 gets a virtio console
			// instead of the emulated UART on stdio.
			name: "hvc0 guest console renders virtio console instead of UART",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       "console=hvc0 8250.nr_uarts=0 root=/dev/vda rw " + testCommand,
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"-chardev stdio,id=urunc-console,signal=off",
				"-device virtio-serial-pci",
				"-device virtconsole,chardev=urunc-console",
			},
			mustNotContain: []string{
				"-serial stdio",
			},
		},
		{
			// The exec agent rides vsock: the CID gives the guest a vhost-vsock
			// device even without vAccel.
			name: "AgentVsockCID renders a vhost-vsock device",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				AgentVsockCID: 42,
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"-device vhost-vsock-pci,id=vhost-vsock-pci0,guest-cid=42",
			},
		},
		{
			name: "custom MemSizeB renders -m in MB",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				MemSizeB:      512 * 1000 * 1000,
			},
			unikernel:   &fakeUnikernel{},
			mustContain: []string{"-m 512M"},
		},
		{
			name: "VCPUs set emits -smp",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				VCPUs:         4,
			},
			unikernel:   &fakeUnikernel{},
			mustContain: []string{"-smp 4"},
		},
		{
			name: "Seccomp=true emits the sandbox flag set",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				Seccomp:       true,
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"--sandbox on",
				"obsolete=deny",
				"elevateprivileges=deny",
				"spawn=deny",
				"resourcecontrol=deny",
			},
		},
		{
			name: "generic tap network renders -netdev tap with params",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				Net:           types.NetDevParams{TapDev: "tap0", MAC: "52:54:00:12:34:56", MTU: 1500},
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"-netdev tap",
				"ifname=tap0",
				"host_mtu=1500",
				"mac=52:54:00:12:34:56",
			},
			mustNotContain: []string{"-nic none", "vhost=on"},
		},
		{
			name:  "vhost on emits vhost=on",
			vhost: true,
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				Net:           types.NetDevParams{TapDev: "tap0", MAC: "aa:bb:cc:dd:ee:ff", MTU: 1500},
			},
			unikernel:   &fakeUnikernel{},
			mustContain: []string{"vhost=on"},
		},
		{
			name: "custom MonitorNetCli is used verbatim",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				Net:           types.NetDevParams{TapDev: "tap0"},
			},
			unikernel:      &fakeUnikernel{netCli: []string{"-netdev", "user,id=net0", "-device", "e1000,netdev=net0"}},
			mustContain:    []string{"-netdev user,id=net0", "-device e1000,netdev=net0"},
			mustNotContain: []string{"-netdev tap"},
		},
		{
			name: "InitrdPath renders -initrd",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				InitrdPath:    "/rootfs/initrd.img",
			},
			unikernel:   &fakeUnikernel{},
			mustContain: []string{"-initrd /rootfs/initrd.img"},
		},
		{
			name: "Sharedfs 9pfs appends the guest-supplied CLI verbatim",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				Sharedfs:      types.SharedfsParams{Type: "9pfs", Path: "/srv/share"},
			},
			unikernel: &fakeUnikernel{sharedfsCli: []string{
				"-fsdev", "local,id=rootfs9p,security_model=none,multidevs=remap,path=/srv/share",
				"-device", "virtio-9p-pci,fsdev=rootfs9p,mount_tag=fs0",
			}},
			mustContain: []string{
				"-fsdev local,id=rootfs9p,security_model=none,multidevs=remap,path=/srv/share",
				"-device virtio-9p-pci,fsdev=rootfs9p,mount_tag=fs0",
			},
		},
		{
			name: "Sharedfs 9pfs with a guest that supplies no CLI renders nothing",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				Sharedfs:      types.SharedfsParams{Type: "9pfs", Path: "/srv/share"},
			},
			unikernel:      &fakeUnikernel{},
			mustNotContain: []string{"-fsdev", "virtio-9p"},
		},
		{
			name: "Sharedfs virtiofs renders memory-backend-file and vhost-user-fs-pci",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				Sharedfs:      types.SharedfsParams{Type: "virtiofs", Path: "/srv/share"},
			},
			unikernel:   &fakeUnikernel{},
			mustContain: []string{"memory-backend-file", "vhost-user-fs-pci"},
		},
		{
			name: "generic block device renders virtio-blk and drive flags",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			unikernel: &fakeUnikernel{blockCli: []types.MonitorBlockArgs{{ID: "data0", Path: "/disks/data0.img"}}},
			mustContain: []string{
				"-device virtio-blk-pci,serial=data0,drive=data0,scsi=off",
				"-drive format=raw,if=none,id=data0,file=/disks/data0.img",
			},
		},
		{
			name: "block device ExactArgs is used verbatim",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			unikernel:      &fakeUnikernel{blockCli: []types.MonitorBlockArgs{{ExactArgs: []string{"-hda", "/custom/disk.img"}}}},
			mustContain:    []string{"-hda /custom/disk.img"},
			mustNotContain: []string{"virtio-blk-pci"},
		},
		{
			name: "MonitorCli ExtraInitrd is appended as -initrd",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			unikernel:   &fakeUnikernel{monitorCli: types.MonitorCliArgs{ExtraInitrd: "/extra/initrd.cpio"}},
			mustContain: []string{"-initrd /extra/initrd.cpio"},
		},
		{
			name: "MonitorCli OtherArgs are appended verbatim",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			unikernel:   &fakeUnikernel{monitorCli: types.MonitorCliArgs{OtherArgs: []string{"-nographic", "-no-reboot"}}},
			mustContain: []string{"-nographic", "-no-reboot"},
		},
		{
			name: "VAccel vsock emits vhost-vsock-pci with the configured guest CID",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				VAccelType:    "vsock",
				VSockDevID:    42,
			},
			unikernel:   &fakeUnikernel{},
			mustContain: []string{"vhost-vsock-pci", "guest-cid=42"},
		},
		{
			// The kernel command line must come out as -append followed by the
			// full command as a single argument. Asserting the whole substring
			// in the joined output is enough to confirm that.
			name: "kernel command is appended as a single argument after -append",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       "init=/bin/sh root=/dev/vda console=ttyS0",
			},
			unikernel:   &fakeUnikernel{},
			mustContain: []string{"-append init=/bin/sh root=/dev/vda console=ttyS0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := &Qemu{binary: QemuBinary, binaryPath: testQemuBinary, vhost: tt.vhost}
			out, err := q.BuildExecCmd(tt.args, tt.unikernel)
			assert.NoError(t, err)
			assert.NotEmpty(t, out)

			// Invariants that hold for every call to BuildExecCmd.
			assert.Equal(t, testQemuBinary, out[0], "binary path must be the first element")
			joined := strings.Join(out, " ")
			if archShouldBePresent {
				assert.Contains(t, joined, archFlag, "expected arch flag to be present")
			} else {
				assert.NotContains(t, joined, archFlag, "expected arch flag to be absent")
			}

			for _, want := range tt.mustContain {
				assert.Contains(t, joined, want, "expected %q to be present", want)
			}
			for _, notWant := range tt.mustNotContain {
				assert.NotContains(t, joined, notWant, "expected %q to be absent", notWant)
			}
		})
	}
}

func TestQemuUnikernelPathIsAlwaysOneArgvElement(t *testing.T) {
	t.Parallel()

	annotationValue := "/unikernel/app.bin -fsdev local,id=extra,path=/data " +
		"-device virtio-9p-pci,fsdev=extra,mount_tag=data"

	q := &Qemu{binary: QemuBinary, binaryPath: testQemuBinary}
	got, err := q.BuildExecCmd(types.ExecArgs{
		UnikernelPath: annotationValue,
		MemSizeB:      256 * 1024 * 1024,
	}, &fakeUnikernel{})
	assert.NoError(t, err)

	kernelIdx := slices.Index(got, "-kernel")
	if kernelIdx == -1 || kernelIdx+1 >= len(got) {
		t.Fatalf("-kernel must be present and followed by the kernel path, got %v", got)
	}
	assert.Equal(t, annotationValue, got[kernelIdx+1], "the annotation value must stay one argv element")
	for _, flag := range []string{"-fsdev", "-device"} {
		assert.NotContains(t, got, flag, "the annotation value must not become a separate flag")
	}
}

func TestQemuGuestCommandIsASingleAppendValue(t *testing.T) {
	t.Parallel()

	q := &Qemu{binary: QemuBinary, binaryPath: testQemuBinary}
	got, err := q.BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		MemSizeB:      256 * 1024 * 1024,
		Command:       "console=ttyS0 root=/dev/vda",
	}, &fakeUnikernel{})
	assert.NoError(t, err)

	if len(got) < 2 {
		t.Fatalf("expected at least -append and its value, got %v", got)
	}
	assert.Equal(t, "-append", got[len(got)-2])
	assert.Equal(t, "console=ttyS0 root=/dev/vda", got[len(got)-1],
		"qemu must receive the guest cmdline as a single element")
}

// TestQemuBuildExecCmdLinuxGolden pins the whole argv of the Linux/KVM
// backend for a guest with every Linux feature at once: a QMP socket, a
// virtio console, a tap device, a block device, an initrd, virtiofs, an
// extra initrd and the agent's vsock device. The darwin fields of Qemu
// must not change a single element of it.
func TestQemuBuildExecCmdLinuxGolden(t *testing.T) {
	t.Parallel()

	q := &Qemu{binary: QemuBinary, binaryPath: testQemuBinary, vhost: true}
	got, err := q.BuildExecCmd(types.ExecArgs{
		ContainerID:   "golden",
		UnikernelPath: testKernelPath,
		InitrdPath:    "/rootfs/initrd.img",
		Command:       "console=hvc0 8250.nr_uarts=0 root=/dev/vda rw",
		MemSizeB:      512 * 1000 * 1000,
		VCPUs:         2,
		Seccomp:       true,
		SocketPath:    "/run/urunc/golden.sock",
		AgentVsockCID: 7,
		Net:           types.NetDevParams{TapDev: "tap0_urunc", MAC: "52:54:00:12:34:56", MTU: 1500},
		Sharedfs:      types.SharedfsParams{Type: "virtiofs", Path: "/srv/share"},
	}, &fakeUnikernel{
		blockCli:   []types.MonitorBlockArgs{{ID: "data0", Path: "/disks/data0.img"}},
		monitorCli: types.MonitorCliArgs{ExtraInitrd: "/urunit.conf", OtherArgs: []string{"-no-reboot"}},
	})
	assert.NoError(t, err)

	machine := []string{}
	netDev := "virtio-net-pci"
	if runtime.GOARCH == "arm64" {
		machine = []string{"-M", "virt"}
		netDev = "virtio-net-device"
	}
	want := []string{
		testQemuBinary,
		"-m", "512M",
		"-L", "/usr/share/qemu",
		"-cpu", "host",
		"-enable-kvm",
		"-display", "none",
		"-vga", "none",
		"-monitor", "null",
		"-qmp", "unix:/run/urunc/golden.sock,server=on,wait=off",
		"-chardev", "stdio,id=urunc-console,signal=off",
		"-device", "virtio-serial-pci",
		"-device", "virtconsole,chardev=urunc-console",
		"-smp", "2",
		"--sandbox", "on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny",
	}
	want = append(want, machine...)
	want = append(want,
		"-kernel", testKernelPath,
		"-netdev", "tap,id=net0,script=no,downscript=no,ifname=tap0_urunc,vhost=on",
		"-device", netDev+",netdev=net0,host_mtu=1500,mac=52:54:00:12:34:56",
		"-device", "virtio-blk-pci,serial=data0,drive=data0,scsi=off",
		"-drive", "format=raw,if=none,id=data0,file=/disks/data0.img",
		"-initrd", "/rootfs/initrd.img",
		"-object", "memory-backend-file,id=mem,size=512M,mem-path=/tmp,share=on",
		"-numa", "node,memdev=mem",
		"-chardev", "socket,id=char0,path=/tmp/vhostqemu",
		"-device", "vhost-user-fs-pci,queue-size=1024,chardev=char0,tag=fs0",
		"-initrd", "/urunit.conf",
		"-no-reboot",
		"-device", "vhost-vsock-pci,id=vhost-vsock-pci0,guest-cid=7",
		"-append", "console=hvc0 8250.nr_uarts=0 root=/dev/vda rw",
	)
	assert.Equal(t, want, got)
}

// qmpOptions returns the value of every -qmp option in argv.
func qmpOptions(argv []string) []string {
	var opts []string
	for i, arg := range argv {
		if arg == "-qmp" && i+1 < len(argv) {
			opts = append(opts, argv[i+1])
		}
	}
	return opts
}

// QEMU takes a single -qmp. ExecArgs.SocketPath (Linux) wins over a socket
// set on the monitor (hull on darwin), and the monitor's socket is used only
// when SocketPath is empty.
func TestQemuQMPSocketPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		qmpSocket  string
		socketPath string
		want       []string
	}{
		{
			name:       "SocketPath wins over SetQMPSocket",
			qmpSocket:  "/tmp/hull-qmp.sock",
			socketPath: "/run/urunc/q.sock",
			want:       []string{"unix:/run/urunc/q.sock,server=on,wait=off"},
		},
		{
			name:      "SetQMPSocket alone renders its socket",
			qmpSocket: "/tmp/hull-qmp.sock",
			want:      []string{"unix:/tmp/hull-qmp.sock,server,nowait"},
		},
		{
			name: "neither renders no -qmp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := &Qemu{binary: QemuBinary, binaryPath: testQemuBinary}
			q.SetQMPSocket(tt.qmpSocket)
			assert.Equal(t, tt.qmpSocket, q.GetQMPSocket())
			got, err := q.BuildExecCmd(types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				SocketPath:    tt.socketPath,
			}, &fakeUnikernel{})
			assert.NoError(t, err)
			assert.Equal(t, tt.want, qmpOptions(got))
		})
	}
}

// PreExec removes a stale QMP socket, and a missing one is not an error.
func TestQemuPreExecRemovesStaleQMPSocket(t *testing.T) {
	t.Parallel()

	sock := filepath.Join(t.TempDir(), "qmp.sock")
	assert.NoError(t, os.WriteFile(sock, nil, 0o600))

	q := &Qemu{}
	q.SetQMPSocket(sock)
	assert.NoError(t, q.PreExec(types.ExecArgs{}))
	_, err := os.Stat(sock)
	assert.True(t, os.IsNotExist(err), "stale QMP socket must be removed")
	assert.NoError(t, q.PreExec(types.ExecArgs{}))
}

// The zero value is the Linux/KVM backend: the fields only the darwin
// backends read change nothing in its argv.
func TestQemuLinuxIgnoresDarwinFields(t *testing.T) {
	t.Parallel()

	q := &Qemu{binary: QemuBinary, binaryPath: testQemuBinary}
	assert.True(t, q.UsesKVM())
	base := types.ExecArgs{UnikernelPath: testKernelPath, Command: testCommand}
	want, err := q.BuildExecCmd(base, &fakeUnikernel{})
	assert.NoError(t, err)

	args := base
	args.RootfsPath = t.TempDir()
	args.BlockDevPath = "/disks/root.img"
	args.SharedDirs = []types.SharedDirParams{{Path: "/srv/data", Tag: "data"}}
	args.Net.UnixSocket = "/tmp/gw.sock"
	args.GUI = true
	args.GUITitle = "guest"
	got, err := q.BuildExecCmd(args, &fakeUnikernel{})
	assert.NoError(t, err)
	assert.Equal(t, want, got)

	q.SetBinaryPath("/opt/qemu/bin/qemu-system-x86_64")
	assert.Equal(t, "/opt/qemu/bin/qemu-system-x86_64", q.Path())
}
