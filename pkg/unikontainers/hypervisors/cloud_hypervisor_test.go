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
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/internal/constants"
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

// The monitor runs as the container's user, and setting a tap's MTU needs
// CAP_NET_ADMIN, which that user does not have for an image that runs as
// anyone but root. urunc sets the MTU itself while it still can, so asking
// Cloud Hypervisor to set it again fails the boot for every non-root image
// and gains nothing when it works.
func TestCloudHypervisorNetLeavesTheTapMTUAlone(t *testing.T) {
	ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: "/usr/bin/cloud-hypervisor"}
	argv, err := ch.BuildExecCmd(types.ExecArgs{
		UnikernelPath: "/rootfs/unikernel.bin",
		Command:       "init=/bin/sh",
		Net:           types.NetDevParams{TapDev: "tap0", MAC: "52:54:00:12:34:56", MTU: 1500},
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "tap=tap0") || !strings.Contains(joined, "mac=52:54:00:12:34:56") {
		t.Errorf("expected the tap and mac in:\n%s", joined)
	}
	if strings.Contains(joined, "mtu=") {
		t.Errorf("mtu= makes Cloud Hypervisor call SIOCSIFMTU, which a non-root monitor "+
			"cannot do:\n%s", joined)
	}
}

// The monitor binds this socket itself, after urunc has dropped it to the
// container image's user. The rootfs root belongs to root, so a socket there
// cannot be created by an image whose user is anyone else.
func TestCloudHypervisorAgentSocketIsNotAtTheRootfsRoot(t *testing.T) {
	if filepath.Dir(constants.AgentVsockUDSPath) != constants.MonitorSockDir {
		t.Fatalf("the agent socket %s is not in %s, the directory urunc hands to "+
			"the monitor's user", constants.AgentVsockUDSPath, constants.MonitorSockDir)
	}
	ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: "/usr/bin/cloud-hypervisor"}
	argv, err := ch.BuildExecCmd(types.ExecArgs{
		UnikernelPath: "/rootfs/unikernel.bin",
		Command:       "init=/bin/sh",
		AgentVsockCID: 42,
		AgentVsockUDS: constants.AgentVsockUDSPath,
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "socket="+constants.AgentVsockUDSPath) {
		t.Errorf("expected socket=%s in:\n%s", constants.AgentVsockUDSPath, joined)
	}
}
