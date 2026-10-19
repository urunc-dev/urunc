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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const testCHBinary = "/usr/bin/cloud-hypervisor"

func TestCloudHypervisorBuildExecCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		args           types.ExecArgs
		unikernel      types.Unikernel
		mustContain    []string
		mustNotContain []string
	}{
		{
			name: "configured SocketPath renders --api-socket on that path",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				SocketPath:    "/run/urunc/ch.sock",
			},
			unikernel:   &fakeUnikernel{},
			mustContain: []string{"--api-socket path=/run/urunc/ch.sock"},
		},
		{
			name: "unset SocketPath omits --api-socket",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				ContainerID:   "abc123",
			},
			unikernel:      &fakeUnikernel{},
			mustNotContain: []string{"--api-socket"},
		},
		{
			name: "no block device renders no --disk",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			unikernel:      &fakeUnikernel{},
			mustContain:    []string{"--kernel " + testKernelPath},
			mustNotContain: []string{"--disk"},
		},
		{
			name: "a block device names its image type",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			unikernel: &fakeUnikernel{
				blockCli: []types.MonitorBlockArgs{
					{ID: "rootfs", Path: "/dev/mapper/urunc-rootfs"},
				},
			},
			mustContain: []string{
				"--disk path=/dev/mapper/urunc-rootfs,id=rootfs,image_type=raw",
			},
		},
		{
			name: "a block device without an id still names its image type",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			unikernel: &fakeUnikernel{
				blockCli: []types.MonitorBlockArgs{{Path: "/tmp/rootfs.img"}},
			},
			mustContain: []string{"--disk path=/tmp/rootfs.img,image_type=raw"},
		},
		{
			name: "exact block arguments pass through untouched",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			unikernel: &fakeUnikernel{
				blockCli: []types.MonitorBlockArgs{
					{ExactArgs: []string{"--disk", "path=/tmp/custom.img,readonly=on"}},
				},
			},
			mustContain:    []string{"--disk path=/tmp/custom.img,readonly=on"},
			mustNotContain: []string{"image_type"},
		},
		{
			name: "a read-only disk and a serial reach the --disk spec",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			unikernel: &fakeUnikernel{
				blockCli: []types.MonitorBlockArgs{
					{ID: "disk0", Path: "/disks/lower.ext4", Serial: "disk0", ReadOnly: true},
					{ID: "disk1", Path: "/disks/upper.ext4", Serial: "disk1"},
				},
			},
			mustContain: []string{
				"--disk path=/disks/lower.ext4,id=disk0,readonly=on,serial=disk0,image_type=raw",
				"--disk path=/disks/upper.ext4,id=disk1,serial=disk1,image_type=raw",
			},
		},
		{
			name: "virtio-fs exports share the guest memory and get one --fs each",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				MemSizeB:      512 * 1000 * 1000,
				VirtiofsExports: []types.VirtiofsExport{
					{Tag: "share0", Socket: "/tmp/vfs-share0.sock"},
					{Tag: "share1", Socket: "/tmp/vfs-share1.sock"},
				},
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"--memory size=512M,shared=on",
				"--fs tag=share0,socket=/tmp/vfs-share0.sock",
				"--fs tag=share1,socket=/tmp/vfs-share1.sock",
			},
			mustNotContain: []string{"tag=fs0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: testCHBinary}
			out, err := ch.BuildExecCmd(tt.args, tt.unikernel)
			assert.NoError(t, err)
			assert.NotEmpty(t, out)

			assert.Equal(t, testCHBinary, out[0], "binary path must be the first element")
			joined := strings.Join(out, " ")

			for _, want := range tt.mustContain {
				assert.Contains(t, joined, want, "expected %q to be present", want)
			}
			for _, notWant := range tt.mustNotContain {
				assert.NotContains(t, joined, notWant, "expected %q to be absent", notWant)
			}
		})
	}
}

func TestCloudHypervisorBuildExecCmdRefuses(t *testing.T) {
	t.Parallel()

	ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: testCHBinary}
	_, err := ch.BuildExecCmd(types.ExecArgs{UnikernelPath: testKernelPath},
		&fakeUnikernel{blockCli: []types.MonitorBlockArgs{{Path: "/disks/a,b.img"}}})
	assert.ErrorContains(t, err, "comma")

	_, err = ch.BuildExecCmd(types.ExecArgs{
		UnikernelPath:   testKernelPath,
		VirtiofsExports: []types.VirtiofsExport{{Tag: "share0", Socket: "/tmp/s.sock,cache=always"}},
	}, &fakeUnikernel{})
	assert.Error(t, err)
}
