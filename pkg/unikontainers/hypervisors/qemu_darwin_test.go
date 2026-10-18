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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// The darwin-configured Qemu (same type, platform fields set) must emit the
// HVF accelerator, the Homebrew firmware path, and a stream netdev for the
// user-mode gateway — never the KVM/tap Linux flags.
func TestNewQemuDarwinCommand(t *testing.T) {
	q := NewQemuDarwin("/opt/homebrew/bin/qemu-system-aarch64")
	if q.UsesKVM() {
		t.Error("darwin backend must not report KVM")
	}
	args := types.ExecArgs{
		UnikernelPath: "/img/kernel",
		KernelPath:    "/img/kernel",
		Command:       "console=hvc0",
		Net:           types.NetDevParams{UnixSocket: "/tmp/gw.sock", MAC: "52:54:00:aa:bb:cc"},
	}
	out, err := q.BuildExecCmd(args, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	for _, want := range []string{"-accel hvf", "-L /opt/homebrew/share/qemu", "-netdev stream", "-qmp unix:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in darwin command:\n%s", want, joined)
		}
	}
	for _, notWant := range []string{"-enable-kvm", "-netdev tap"} {
		if strings.Contains(joined, notWant) {
			t.Errorf("unexpected Linux flag %q in darwin command", notWant)
		}
	}
}

// hull names the QEMU/HVF backend "qemu-hvf"; it and "qemu" build the darwin
// Qemu, which does not use KVM.
func TestNewVMMQemuHVF(t *testing.T) {
	if !IsHVFAvailable() {
		t.Skip("HVF needs Apple Silicon")
	}

	monitors := map[string]types.MonitorConfig{"qemu-hvf": {BinaryPath: "/opt/qemu/bin/qemu-system-aarch64"}}
	for _, name := range []VmmType{QemuHvfAlias, QemuVmm} {
		vmm, err := NewVMM(name, monitors)
		assert.NoError(t, err)
		q, ok := vmm.(*Qemu)
		if !assert.True(t, ok, "NewVMM(%q) must return a *Qemu, got %T", name, vmm) {
			continue
		}
		assert.False(t, q.UsesKVM())
		assert.Equal(t, "/opt/qemu/bin/qemu-system-aarch64", q.Path())
	}

	if _, err := darwinQemuPath(nil); err != nil {
		t.Skipf("no qemu-system-aarch64 on this host: %v", err)
	}
	vmm, err := NewVMM("qemu-hvf", nil)
	assert.NoError(t, err)
	q, ok := vmm.(*Qemu)
	if assert.True(t, ok, "NewVMM(qemu-hvf, nil) must return a *Qemu, got %T", vmm) {
		assert.False(t, q.UsesKVM())
	}
}

// The darwin registry has no Linux-only monitor.
func TestNewVMMDarwinRejectsLinuxMonitors(t *testing.T) {
	for _, name := range []VmmType{"spt", HvtVmm, FirecrackerVmm, "cloud-hypervisor", "hedge"} {
		_, err := NewVMM(name, nil)
		assert.EqualError(t, err, "vmm \""+string(name)+"\" is not supported")
	}
}

// On darwin QEMU itself attaches the container rootfs: BlockDevPath as a raw
// disk, a RootfsPath directory over 9p under tag rootfs, a RootfsPath file as
// an initrd, and one 9p export per SharedDirs entry.
func TestQemuDarwinRootfsArgs(t *testing.T) {
	q := NewQemuDarwin("/opt/homebrew/bin/qemu-system-aarch64")
	dir := t.TempDir()
	file := filepath.Join(dir, "rootfs.cpio")
	assert.NoError(t, os.WriteFile(file, nil, 0o600))

	tests := []struct {
		name string
		args types.ExecArgs
		want []string
	}{
		{
			name: "block device",
			args: types.ExecArgs{BlockDevPath: "/img/root.img", RootfsPath: dir},
			want: []string{"-drive format=raw,file=/img/root.img,if=virtio"},
		},
		{
			name: "directory",
			args: types.ExecArgs{RootfsPath: dir},
			want: []string{
				"-fsdev local,id=rootdev,security_model=none,path=" + dir,
				"-device virtio-9p-pci,fsdev=rootdev,mount_tag=rootfs",
			},
		},
		{
			name: "file",
			args: types.ExecArgs{RootfsPath: file},
			want: []string{"-initrd " + file},
		},
		{
			name: "shared dirs",
			args: types.ExecArgs{SharedDirs: []types.SharedDirParams{{Path: "/host/data", Tag: "data"}}},
			want: []string{
				"-fsdev local,id=data,security_model=none,path=/host/data",
				"-device virtio-9p-pci,fsdev=data,mount_tag=data",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.args.UnikernelPath = "/img/kernel"
			tt.args.Command = "console=hvc0"
			out, err := q.BuildExecCmd(tt.args, &fakeUnikernel{})
			assert.NoError(t, err)
			joined := strings.Join(out, " ")
			for _, want := range tt.want {
				assert.Contains(t, joined, want)
			}
		})
	}
}

// The unikernel names the urunit config by its guest path; darwin has no
// container mount namespace, so QEMU gets it under the host rootfs.
func TestQemuDarwinExtraInitrdUnderSharedfs(t *testing.T) {
	q := NewQemuDarwin("/opt/homebrew/bin/qemu-system-aarch64")
	out, err := q.BuildExecCmd(types.ExecArgs{
		UnikernelPath: "/img/kernel",
		Command:       "console=hvc0",
		Sharedfs:      types.SharedfsParams{Type: "9pfs", Path: "/host/rootfs"},
	}, &fakeUnikernel{monitorCli: types.MonitorCliArgs{ExtraInitrd: "/urunit.conf"}})
	assert.NoError(t, err)
	assert.Contains(t, strings.Join(out, " "), "-initrd /host/rootfs/urunit.conf")
}

// A disk.img next to the unikernel boots with EDK2, without -kernel or
// -append.
func TestQemuDarwinDiskImageBootsWithEDK2(t *testing.T) {
	dir := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "disk.img"), nil, 0o600))

	q := NewQemuDarwin("/opt/homebrew/bin/qemu-system-aarch64")
	out, err := q.BuildExecCmd(types.ExecArgs{
		UnikernelPath: filepath.Join(dir, "kernel"),
		Command:       "console=hvc0",
	}, &fakeUnikernel{})
	assert.NoError(t, err)
	joined := strings.Join(out, " ")
	assert.Contains(t, joined, "-drive format=raw,file="+filepath.Join(dir, "disk.img")+",if=virtio")
	assert.Contains(t, joined, "-bios /opt/homebrew/share/qemu/edk2-aarch64-code.fd")
	assert.NotContains(t, out, "-kernel")
	assert.NotContains(t, out, "-append")
}
