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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const testHviBinary = "/usr/bin/hvi"

// flagValue returns the value following the first occurrence of flag, and
// whether the flag was present at all.
func flagValue(argv []string, flag string) (string, bool) {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1], true
		}
	}
	return "", false
}

// countFlag returns how many times flag appears in argv.
func countFlag(argv []string, flag string) int {
	n := 0
	for _, a := range argv {
		if a == flag {
			n++
		}
	}
	return n
}

func newTestHvi() *Hvi {
	return &Hvi{binaryPath: testHviBinary, binary: HviBinary}
}

func TestHviBuildExecCmdBase(t *testing.T) {
	t.Parallel()

	argv, err := newTestHvi().BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		Command:       testCommand,
		MemSizeB:      512 << 20,
		VCPUs:         2,
		Seccomp:       true,
		ContainerID:   "abc123",
	}, &fakeUnikernel{})
	require.NoError(t, err)

	assert.Equal(t, []string{testHviBinary, "boot"}, argv[:2])
	for flag, want := range map[string]string{
		"--kernel":     testKernelPath,
		"--mem-mib":    "512",
		"--cpus":       "2",
		"--sandbox-id": "abc123",
		"--cmdline":    testCommand,
	} {
		got, ok := flagValue(argv, flag)
		assert.True(t, ok, "expected %s in %v", flag, argv)
		assert.Equal(t, want, got, flag)
	}
	for _, flag := range []string{"--no-sandbox", "--disk", "--fs", "--net-tap", "--agent-sock", "--initramfs"} {
		assert.Zero(t, countFlag(argv, flag), "%s in %v", flag, argv)
	}

	argv, err = newTestHvi().BuildExecCmd(types.ExecArgs{UnikernelPath: testKernelPath}, &fakeUnikernel{})
	require.NoError(t, err)
	assert.Equal(t, 1, countFlag(argv, "--no-sandbox"), "seccomp off maps to --no-sandbox: %v", argv)
}

// A lone writable disk with no serial is the block rootfs and keeps the plain
// --disk <path> every hvi takes. Anything else uses the keyed form.
func TestHviBuildExecCmdDisk(t *testing.T) {
	t.Parallel()

	argv, err := newTestHvi().BuildExecCmd(types.ExecArgs{UnikernelPath: testKernelPath},
		&fakeUnikernel{blockCli: []types.MonitorBlockArgs{{ID: "rootfs", Path: "/dev/dm-3"}}})
	require.NoError(t, err)
	got, ok := flagValue(argv, "--disk")
	assert.True(t, ok, "expected --disk in %v", argv)
	assert.Equal(t, "/dev/dm-3", got)

	argv, err = newTestHvi().BuildExecCmd(types.ExecArgs{UnikernelPath: testKernelPath},
		&fakeUnikernel{blockCli: []types.MonitorBlockArgs{
			{ID: "rootfs", Path: "/dev/dm-3"},
			{ID: "data", Path: "/dev/dm-4", Serial: "data"},
		}})
	require.NoError(t, err)
	assert.Equal(t, []string{"--disk", "path=/dev/dm-3", "--disk", "path=/dev/dm-4,serial=data"},
		flagPairs(argv, "--disk"))

	argv, err = newTestHvi().BuildExecCmd(types.ExecArgs{UnikernelPath: testKernelPath},
		&fakeUnikernel{blockCli: []types.MonitorBlockArgs{
			{ID: "disk0", Path: "/disks/lower.ext4", Serial: "disk0", ReadOnly: true},
			{ID: "disk1", Path: "/disks/upper.ext4", Serial: "disk1"},
		}})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"--disk", "path=/disks/lower.ext4,readonly=on,serial=disk0",
		"--disk", "path=/disks/upper.ext4,serial=disk1",
	}, flagPairs(argv, "--disk"))

	// A single read-only disk is not the plain rootfs form.
	argv, err = newTestHvi().BuildExecCmd(types.ExecArgs{UnikernelPath: testKernelPath},
		&fakeUnikernel{blockCli: []types.MonitorBlockArgs{{Path: "/disks/lower.ext4", ReadOnly: true}}})
	require.NoError(t, err)
	assert.Equal(t, []string{"--disk", "path=/disks/lower.ext4,readonly=on"}, flagPairs(argv, "--disk"))

	_, err = newTestHvi().BuildExecCmd(types.ExecArgs{UnikernelPath: testKernelPath},
		&fakeUnikernel{blockCli: []types.MonitorBlockArgs{
			{Path: "/disks/a,b.ext4", ReadOnly: true},
		}})
	assert.ErrorContains(t, err, "comma")
}

// flagPairs returns every occurrence of flag in argv with its value, in order.
func flagPairs(argv []string, flag string) []string {
	var out []string
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			out = append(out, a, argv[i+1])
		}
	}
	return out
}

// A virtiofs rootfs is served by the virtiofsd that preStartCmd starts. hvi
// gets the daemon's socket under the tag the boot line mounts as root.
func TestHviBuildExecCmdVirtiofs(t *testing.T) {
	t.Parallel()

	hvi := newTestHvi()
	assert.True(t, hvi.SupportsSharedfs("virtio"))
	assert.False(t, hvi.SupportsSharedfs("9p"))

	argv, err := hvi.BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		Sharedfs:      types.SharedfsParams{Type: "virtiofs", Path: "/rootfs"},
	}, &fakeUnikernel{})
	require.NoError(t, err)
	got, ok := flagValue(argv, "--fs")
	assert.True(t, ok, "expected --fs in %v", argv)
	assert.Equal(t, "tag=fs0,socket=/tmp/vhostqemu", got)
	assert.Equal(t, 1, countFlag(argv, "--fs"), "argv: %v", argv)
}

// Each extra virtio-fs export gets its own --fs on its own daemon's socket,
// after the rootfs one.
func TestHviBuildExecCmdVirtiofsExports(t *testing.T) {
	t.Parallel()

	argv, err := newTestHvi().BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		VirtiofsExports: []types.VirtiofsExport{
			{Tag: "share0", Socket: "/tmp/vfs-share0.sock"},
			{Tag: "share1", Socket: "/tmp/vfs-share1.sock"},
		},
	}, &fakeUnikernel{})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"--fs", "tag=share0,socket=/tmp/vfs-share0.sock",
		"--fs", "tag=share1,socket=/tmp/vfs-share1.sock",
	}, flagPairs(argv, "--fs"))

	for _, bad := range []types.VirtiofsExport{
		{Tag: "", Socket: "/tmp/s.sock"},
		{Tag: "share0", Socket: ""},
		{Tag: "a,b", Socket: "/tmp/s.sock"},
		{Tag: "share0", Socket: "/tmp/s.sock,readonly=on"},
	} {
		_, err = newTestHvi().BuildExecCmd(types.ExecArgs{
			UnikernelPath:   testKernelPath,
			VirtiofsExports: []types.VirtiofsExport{bad},
		}, &fakeUnikernel{})
		assert.Error(t, err, "%+v", bad)
	}
}

// hvi's built-in stack (--net-stub, once --net) forwards no traffic, so a
// container's guest goes on its tap.
func TestHviBuildExecCmdTap(t *testing.T) {
	t.Parallel()

	argv, err := newTestHvi().BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		Net:           types.NetDevParams{TapDev: "tap0_urunc", MAC: "02:00:00:00:00:01"},
	}, &fakeUnikernel{})
	require.NoError(t, err)
	tap, ok := flagValue(argv, "--net-tap")
	assert.True(t, ok, "expected --net-tap in %v", argv)
	assert.Equal(t, "tap0_urunc", tap)
	mac, ok := flagValue(argv, "--net-mac")
	assert.True(t, ok, "expected --net-mac in %v", argv)
	assert.Equal(t, "02:00:00:00:00:01", mac)
	assert.Zero(t, countFlag(argv, "--net"), "argv: %v", argv)
	assert.Zero(t, countFlag(argv, "--net-stub"), "argv: %v", argv)
}

func TestHviBuildExecCmdAgentSock(t *testing.T) {
	t.Parallel()

	argv, err := newTestHvi().BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		AgentVsockCID: 1234,
		AgentVsockUDS: "/urunc-agent.vsock",
	}, &fakeUnikernel{})
	require.NoError(t, err)
	got, ok := flagValue(argv, "--agent-sock")
	assert.True(t, ok, "expected --agent-sock in %v", argv)
	assert.Equal(t, "/urunc-agent.vsock", got)

	argv, err = newTestHvi().BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		AgentVsockUDS: "/urunc-agent.vsock",
	}, &fakeUnikernel{})
	require.NoError(t, err)
	assert.Zero(t, countFlag(argv, "--agent-sock"), "no CID means no agent: %v", argv)
}

// hvi takes one initrd. The urunit configuration fills the slot when the
// guest has no other initrd, and two are refused.
func TestHviBuildExecCmdInitrd(t *testing.T) {
	t.Parallel()

	argv, err := newTestHvi().BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		InitrdPath:    "/boot/initrd.urunc",
	}, &fakeUnikernel{})
	require.NoError(t, err)
	got, _ := flagValue(argv, "--initramfs")
	assert.Equal(t, "/boot/initrd.urunc", got)

	argv, err = newTestHvi().BuildExecCmd(types.ExecArgs{UnikernelPath: testKernelPath},
		&fakeUnikernel{monitorCli: types.MonitorCliArgs{ExtraInitrd: "/urunit.conf"}})
	require.NoError(t, err)
	got, _ = flagValue(argv, "--initramfs")
	assert.Equal(t, "/urunit.conf", got)
	assert.Equal(t, 1, countFlag(argv, "--initramfs"), "argv: %v", argv)

	_, err = newTestHvi().BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		InitrdPath:    "/boot/initrd.urunc",
	}, &fakeUnikernel{monitorCli: types.MonitorCliArgs{ExtraInitrd: "/urunit.conf"}})
	require.Error(t, err)
}

func TestHviBuildExecCmdRefuses(t *testing.T) {
	t.Parallel()

	_, err := newTestHvi().BuildExecCmd(types.ExecArgs{}, &fakeUnikernel{})
	assert.Error(t, err, "no kernel")

	_, err = newTestHvi().BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		VAccelType:    "vsock",
	}, &fakeUnikernel{})
	assert.ErrorContains(t, err, "vAccel")
}
