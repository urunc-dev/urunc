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

// A read-only tagged share must go out over --share-ro, and a read-write one
// over --share. The flag carries the mode rather than a third positional, so
// the argument shape stays fixed; getting this wrong would hand the guest a
// writable mount of a directory the caller asked to protect.
func TestVzDarwinSharedDirReadOnly(t *testing.T) {
	args := types.ExecArgs{
		UnikernelPath: "/img/kernel",
		KernelPath:    "/img/kernel",
		Command:       "console=hvc0",
		SharedDirs: []types.SharedDirParams{
			{Path: "/host/rw", Tag: "rwtag"},
			{Path: "/host/ro", Tag: "rotag", ReadOnly: true},
		},
	}
	out, err := NewVzDarwin().BuildExecCmd(args, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	if !strings.Contains(joined, "--share /host/rw rwtag") {
		t.Errorf("read-write share should use --share:\n%s", joined)
	}
	if !strings.Contains(joined, "--share-ro /host/ro rotag") {
		t.Errorf("read-only share should use --share-ro:\n%s", joined)
	}
	// The dangerous regression: a read-only share silently emitted as --share.
	if strings.Contains(joined, "--share /host/ro") {
		t.Errorf("read-only share was emitted read-write:\n%s", joined)
	}
}

// Legacy root shares remain writable by default. Generic container boot opts
// into a read-only lower layer explicitly in the next test.
func TestVzDarwinRootShareStaysWritable(t *testing.T) {
	args := types.ExecArgs{
		UnikernelPath: "/img/kernel",
		KernelPath:    "/img/kernel",
		Command:       "console=hvc0",
		Sharedfs:      types.SharedfsParams{Path: "/host/root"},
		SharedDirs:    []types.SharedDirParams{{Path: "/host/ro", Tag: "rotag", ReadOnly: true}},
	}
	out, err := NewVzDarwin().BuildExecCmd(args, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	if !strings.Contains(joined, "--share /host/root fs0") {
		t.Errorf("root share must stay read-write:\n%s", joined)
	}
}

func TestVzDarwinContainerBootRootShareIsTaggedReadOnly(t *testing.T) {
	args := types.ExecArgs{
		UnikernelPath: "/host/Image",
		KernelPath:    "/host/Image",
		InitrdPath:    "/instance/container-initrd",
		Command:       "rdinit=/vz-init console=hvc0",
		Sharedfs: types.SharedfsParams{
			Path: "/store/ubuntu/rootfs", Tag: "rootfs", ReadOnly: true,
		},
	}
	out, err := NewVzDarwin().BuildExecCmd(args, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	for _, want := range []string{
		"--kernel /host/Image",
		"--initrd /instance/container-initrd",
		"--share-ro /store/ubuntu/rootfs rootfs",
		"--cmdline rdinit=/vz-init console=hvc0",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in Vz command:\n%s", want, joined)
		}
	}
}

// The whole argv for a disk-backed guest with a MAC, a legacy RootfsPath
// that the block device overrides, and a QMP socket.
func TestVzDarwinCommand(t *testing.T) {
	v := &VzDarwin{vzRunnerPath: "/opt/hull/vz-runner"}
	v.SetQMPSocket("/run/hull/vz.qmp")
	out, err := v.BuildExecCmd(types.ExecArgs{
		KernelPath:   "/img/Image",
		MemSizeB:     512 * 1024 * 1024,
		VCPUs:        2,
		Command:      "console=hvc0 root=/dev/vda",
		BlockDevPath: "/img/root.img",
		RootfsPath:   t.TempDir(),
		Net:          types.NetDevParams{MAC: "52:54:00:aa:bb:cc"},
	}, &fakeUnikernel{})
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"/opt/hull/vz-runner",
		"--kernel", "/img/Image",
		"--mem", "512",
		"--cpus", "2",
		"--cmdline", "console=hvc0 root=/dev/vda",
		"--mac", "52:54:00:aa:bb:cc",
		"--rootfs", "/img/root.img",
		"--qmp", "/run/hull/vz.qmp",
	}, out)
}

// vz-runner relays the agent socket to the guest's vsock port, after --qmp.
func TestVzDarwinAgentSock(t *testing.T) {
	v := &VzDarwin{vzRunnerPath: "/opt/hull/vz-runner"}
	v.SetQMPSocket("/run/hull/vz.qmp")
	out, err := v.BuildExecCmd(types.ExecArgs{
		KernelPath:    "/img/Image",
		AgentSockPath: "/run/hull/agent.sock",
	}, &fakeUnikernel{})
	assert.NoError(t, err)
	assert.Equal(t, []string{"--qmp", "/run/hull/vz.qmp", "--agent-sock", "/run/hull/agent.sock"}, out[len(out)-4:])
}

// A legacy RootfsPath directory is shared under tag rootfs.
func TestVzDarwinLegacyRootfsShare(t *testing.T) {
	dir := t.TempDir()
	out, err := NewVzDarwin().BuildExecCmd(types.ExecArgs{
		KernelPath: "/img/Image",
		RootfsPath: dir,
	}, &fakeUnikernel{})
	assert.NoError(t, err)
	assert.Contains(t, strings.Join(out, " "), "--share "+dir+" rootfs")
}

func TestVzDarwinNeedsKernelPath(t *testing.T) {
	_, err := NewVzDarwin().BuildExecCmd(types.ExecArgs{UnikernelPath: "/img/kernel"}, &fakeUnikernel{})
	assert.EqualError(t, err, "kernel path required for Vz backend")
}

func TestVzDarwinCapabilities(t *testing.T) {
	v := NewVzDarwin()
	assert.False(t, v.UsesKVM())
	assert.True(t, v.SupportsSharedfs("virtiofs"))
	assert.True(t, v.SupportsControlSocket())

	exe, err := os.Executable()
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(filepath.Dir(exe), "vz-runner"), v.Path())
}

// PreExec creates the directory of the QMP socket.
func TestVzDarwinPreExecCreatesQMPDir(t *testing.T) {
	sockDir := filepath.Join(t.TempDir(), "run", "hull")
	v := NewVzDarwin()
	v.SetQMPSocket(filepath.Join(sockDir, "vz.qmp"))
	assert.NoError(t, v.PreExec(types.ExecArgs{}))
	info, err := os.Stat(sockDir)
	assert.NoError(t, err)
	assert.True(t, info.IsDir())
}

// NewVMM("vz") fails only because no vz-runner sits next to the test binary.
func TestNewVMMVz(t *testing.T) {
	runner := NewVzDarwin().Path()
	if _, err := os.Stat(runner); err == nil {
		vmm, err := NewVMM(VzVmm, nil)
		assert.NoError(t, err)
		assert.IsType(t, &VzDarwin{}, vmm)
		return
	}
	_, err := NewVMM(VzVmm, nil)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "vz-runner not found at "+runner+": ")
	}
}
