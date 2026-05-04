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

func newTestCH() *CloudHypervisor {
	return &CloudHypervisor{
		binary:     CloudHypervisorBinary,
		binaryPath: "/usr/bin/cloud-hypervisor",
	}
}

func TestCloudHypervisorUsesKVM(t *testing.T) {
	t.Parallel()
	assert.True(t, newTestCH().UsesKVM())
}

func TestCloudHypervisorOk(t *testing.T) {
	t.Parallel()
	assert.NoError(t, newTestCH().Ok())
}

func TestCloudHypervisorPath(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "/usr/bin/cloud-hypervisor", newTestCH().Path())
}

func TestCloudHypervisorPreExec(t *testing.T) {
	t.Parallel()
	assert.NoError(t, newTestCH().PreExec(types.ExecArgs{}))
}

func TestCloudHypervisorSupportsSharedfs(t *testing.T) {
	t.Parallel()
	ch := newTestCH()
	assert.True(t, ch.SupportsSharedfs("virtio"))
	assert.False(t, ch.SupportsSharedfs("9p"))
	assert.False(t, ch.SupportsSharedfs(""))
}

func TestCloudHypervisorBuildExecCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        types.ExecArgs
		unikernel   *mockUnikernel
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "default memory when MemSizeB is zero",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
				Command:       "console=ttyS0",
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--memory", "size=256M", "--kernel", "/kernel", "--cmdline", "console=ttyS0"},
			wantAbsent:  []string{"shared=on"},
		},
		{
			name: "memory shared=on when virtiofs",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
				MemSizeB:      512 * 1000 * 1000,
				Sharedfs:      types.SharedfsParams{Type: "virtiofs"},
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--memory", "size=512M,shared=on"},
		},
		{
			name: "vcpus flag set when VCPUs > 0",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
				VCPUs:         4,
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--cpus", "boot=4"},
		},
		{
			name: "seccomp enabled",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
				Seccomp:       true,
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--seccomp", "true"},
		},
		{
			name: "seccomp disabled",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--seccomp", "false"},
		},
		{
			name: "default net config when MonitorNetCli returns empty",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
				Net: types.NetDevParams{
					TapDev: "tap0",
					MAC:    "aa:bb:cc:dd:ee:ff",
					MTU:    1500,
				},
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--net", "tap=tap0,mac=aa:bb:cc:dd:ee:ff,mtu=1500"},
		},
		{
			name: "unikernel net cli used when MonitorNetCli returns non-empty",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
				Net: types.NetDevParams{
					TapDev: "tap0",
					MAC:    "aa:bb:cc:dd:ee:ff",
				},
			},
			unikernel:   &mockUnikernel{netCli: "--net custom=arg"},
			wantContain: []string{"--net", "custom=arg"},
			wantAbsent:  []string{"tap=tap0"},
		},
		{
			name: "no net args when TapDev is empty",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
			},
			unikernel:  &mockUnikernel{},
			wantAbsent: []string{"--net"},
		},
		{
			name: "block device with ExactArgs",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
			},
			unikernel: &mockUnikernel{
				blockCli: []types.MonitorBlockArgs{
					{ExactArgs: "--disk path=/dev/vda"},
				},
			},
			wantContain: []string{"--disk", "path=/dev/vda"},
		},
		{
			name: "block device with Path and ID",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
			},
			unikernel: &mockUnikernel{
				blockCli: []types.MonitorBlockArgs{
					{Path: "/dev/vda", ID: "disk0"},
				},
			},
			wantContain: []string{"--disk", "path=/dev/vda,id=disk0"},
		},
		{
			name: "block device with Path only",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
			},
			unikernel: &mockUnikernel{
				blockCli: []types.MonitorBlockArgs{
					{Path: "/dev/vda"},
				},
			},
			wantContain: []string{"--disk", "path=/dev/vda"},
			wantAbsent:  []string{",id="},
		},
		{
			name: "initrd path included",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
				InitrdPath:    "/initrd.img",
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--initramfs", "/initrd.img"},
		},
		{
			name: "extra initrd from MonitorCli",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
			},
			unikernel: &mockUnikernel{
				monCli: types.MonitorCliArgs{ExtraInitrd: "/extra-initrd.img"},
			},
			wantContain: []string{"--initramfs", "/extra-initrd.img"},
		},
		{
			name: "virtiofs sharedfs adds fs flag",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
				Sharedfs:      types.SharedfsParams{Type: "virtiofs"},
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--fs", "tag=fs0,socket=/tmp/vhostqemu"},
		},
		{
			name: "vsock flag when VAccelType is vsock",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
				VAccelType:    "vsock",
				VSockDevID:    42,
				VSockDevPath:  "/run/vaccel",
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--vsock", "cid=42,socket=/run/vaccel/vaccel.sock"},
		},
		{
			name: "other args from MonitorCli appended",
			args: types.ExecArgs{
				UnikernelPath: "/kernel",
			},
			unikernel: &mockUnikernel{
				monCli: types.MonitorCliArgs{OtherArgs: "--extra flag"},
			},
			wantContain: []string{"--extra", "flag"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ch := newTestCH()
			got, err := ch.BuildExecCmd(tc.args, tc.unikernel)
			assert.NoError(t, err)
			assert.Equal(t, "/usr/bin/cloud-hypervisor", got[0], "first element must be the binary path")

			joined := strings.Join(got, " ")
			for _, want := range tc.wantContain {
				assert.Contains(t, joined, want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, joined, absent)
			}
		})
	}
}
