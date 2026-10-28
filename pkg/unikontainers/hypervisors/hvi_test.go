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

func TestHviBuildExecCmd_MissingKernel(t *testing.T) {
	t.Parallel()

	h := &HVI{binary: HviBinary, binaryPath: testHviBinary}
	_, err := h.BuildExecCmd(types.ExecArgs{}, &fakeUnikernel{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kernel path is required")
}

func TestHviBuildExecCmd(t *testing.T) {
	t.Parallel()

	h := &HVI{
		binary:     HviBinary,
		binaryPath: testHviBinary,
	}

	testCases := []struct {
		name      string
		args      types.ExecArgs
		unikernel types.Unikernel
		expected  []string
	}{
		{
			name: "kernel, memory, vcpus, cmdline",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				MemSizeB:      512 * 1024 * 1024,
				VCPUs:         2,
				Command:       "panic=-1 console=ttyS0",
				Seccomp:       true,
			},
			unikernel: &fakeUnikernel{},
			expected: []string{
				testHviBinary, "boot",
				"--kernel", testKernelPath,
				"--mem-mib", "512",
				"--cpus", "2",
				"--cmdline", "panic=-1 console=ttyS0",
			},
		},
		{
			name: "memory below 1 MiB defaults to DefaultMemory",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				MemSizeB:      1024, // < 1 MiB -> bytesToMiB is 0 -> DefaultMemory (256)
				Seccomp:       true,
			},
			unikernel: &fakeUnikernel{},
			expected: []string{
				testHviBinary, "boot",
				"--kernel", testKernelPath,
				"--mem-mib", "256",
			},
		},
		{
			name: "tap networking with mac",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Net: types.NetDevParams{
					TapDev: "tap0",
					MAC:    "52:54:00:12:34:56",
				},
				Seccomp: true,
			},
			unikernel: &fakeUnikernel{},
			expected: []string{
				testHviBinary, "boot",
				"--kernel", testKernelPath,
				"--net-tap", "tap0",
				"--net-mac", "52:54:00:12:34:56",
			},
		},
		{
			name: "block devices and disks with serial",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Seccomp:       true,
			},
			unikernel: &fakeUnikernel{
				blockCli: []types.MonitorBlockArgs{
					{Path: "/path/to/rootfs.raw", ID: "rootfs"},
					{Path: "/path/to/data.raw"},
					{ExactArgs: []string{"--disk", "path=/custom.raw,readonly=on"}},
				},
			},
			expected: []string{
				testHviBinary, "boot",
				"--kernel", testKernelPath,
				"--disk", "path=/path/to/rootfs.raw,serial=rootfs",
				"--disk", "path=/path/to/data.raw",
				"--disk", "path=/custom.raw,readonly=on",
			},
		},
		{
			name: "initramfs from args and extra monitor cli",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				InitrdPath:    "/boot/initrd.cpio",
				Seccomp:       true,
			},
			unikernel: &fakeUnikernel{
				monitorCli: types.MonitorCliArgs{
					ExtraInitrd: "/extra/initrd.cpio",
				},
			},
			expected: []string{
				testHviBinary, "boot",
				"--kernel", testKernelPath,
				"--initramfs", "/boot/initrd.cpio",
			},
		},
		{
			name: "initramfs fallback to extra monitor cli when args initrd is empty",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Seccomp:       true,
			},
			unikernel: &fakeUnikernel{
				monitorCli: types.MonitorCliArgs{
					ExtraInitrd: "/extra/initrd.cpio",
				},
			},
			expected: []string{
				testHviBinary, "boot",
				"--kernel", testKernelPath,
				"--initramfs", "/extra/initrd.cpio",
			},
		},
		{
			name: "virtiofs shared filesystem",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Sharedfs: types.SharedfsParams{
					Type: "virtiofs",
				},
				Seccomp: true,
			},
			unikernel: &fakeUnikernel{},
			expected: []string{
				testHviBinary, "boot",
				"--kernel", testKernelPath,
				"--fs", "tag=fs0,socket=/tmp/vhostqemu",
			},
		},
		{
			name: "disabled seccomp emits --no-sandbox",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Seccomp:       false,
			},
			unikernel: &fakeUnikernel{},
			expected: []string{
				testHviBinary, "boot",
				"--kernel", testKernelPath,
				"--no-sandbox",
			},
		},
		{
			name: "extra other args forwarded verbatim",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Seccomp:       true,
			},
			unikernel: &fakeUnikernel{
				monitorCli: types.MonitorCliArgs{
					OtherArgs: []string{"--trace-io", "/tmp/trace.log"},
				},
			},
			expected: []string{
				testHviBinary, "boot",
				"--kernel", testKernelPath,
				"--trace-io", "/tmp/trace.log",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := h.BuildExecCmd(tc.args, tc.unikernel)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, out)
		})
	}
}

func TestHviSupportsSharedfs(t *testing.T) {
	t.Parallel()

	h := &HVI{}
	assert.True(t, h.SupportsSharedfs("virtio"), "hvi should support virtio sharedfs")
	assert.False(t, h.SupportsSharedfs("9p"), "hvi should not support 9p sharedfs")
	assert.False(t, h.SupportsSharedfs(""), "hvi should not support empty sharedfs type")
}
