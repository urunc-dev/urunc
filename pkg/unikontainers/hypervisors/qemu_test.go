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
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// fakeUnikernel is a minimal stub of types.Unikernel used only to drive
// Qemu.BuildExecCmd. The three Monitor* methods are the ones the function
// consults; the rest return zero values.
type fakeUnikernel struct {
	netCli     string
	blockCli   []types.MonitorBlockArgs
	monitorCli types.MonitorCliArgs
}

func (f *fakeUnikernel) Init(types.UnikernelParams) error          { return nil }
func (f *fakeUnikernel) CommandString() (string, error)            { return "", nil }
func (f *fakeUnikernel) SupportsBlock() bool                       { return true }
func (f *fakeUnikernel) SupportsFS(string) bool                    { return true }
func (f *fakeUnikernel) MonitorNetCli(string, string) string       { return f.netCli }
func (f *fakeUnikernel) MonitorBlockCli() []types.MonitorBlockArgs { return f.blockCli }
func (f *fakeUnikernel) MonitorCli() types.MonitorCliArgs          { return f.monitorCli }

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
			unikernel:      &fakeUnikernel{netCli: " -netdev user,id=net0 -device e1000,netdev=net0"},
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
			name: "Sharedfs 9pfs renders fsdev and virtio-9p-pci",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				Sharedfs:      types.SharedfsParams{Type: "9pfs", Path: "/srv/share"},
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"-fsdev local,id=rootfs9p,security_model=none,path=/srv/share",
				"-device virtio-9p-pci,fsdev=rootfs9p,mount_tag=fs0",
			},
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
			unikernel:      &fakeUnikernel{blockCli: []types.MonitorBlockArgs{{ExactArgs: " -hda /custom/disk.img"}}},
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
			unikernel:   &fakeUnikernel{monitorCli: types.MonitorCliArgs{OtherArgs: " -nographic -no-reboot"}},
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
