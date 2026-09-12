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

type mockUnikernel struct{}

func (m *mockUnikernel) Init(_ types.UnikernelParams) error {
	return nil
}

func (m *mockUnikernel) CommandString() (string, error) {
	return "", nil
}

func (m *mockUnikernel) SupportsBlock() bool {
	return true
}

func (m *mockUnikernel) MonitorCli() types.MonitorCliArgs {
	return types.MonitorCliArgs{}
}

func (m *mockUnikernel) MonitorNetCli(_, _ string) []string {
	return nil
}

func (m *mockUnikernel) MonitorBlockCli() []types.MonitorBlockArgs {
	return nil
}

func (m *mockUnikernel) MonitorSharedfsCli(_, _ string) []string {
	return nil
}

func (m *mockUnikernel) SupportsFS(_ string) bool {
	return true
}

func TestCloudHypervisorSupportsSharedfs(t *testing.T) {
	t.Parallel()
	ch := &CloudHypervisor{}

	tests := []struct {
		fsType   string
		expected bool
	}{
		{"virtio", true},
		{"virtiofs", true},
		{"9p", false},
		{"9pfs", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.fsType, func(t *testing.T) {
			assert.Equal(t, tt.expected, ch.SupportsSharedfs(tt.fsType))
		})
	}
}

func TestCloudHypervisorSimpleMethods(t *testing.T) {
	t.Parallel()
	ch := &CloudHypervisor{
		binary:     CloudHypervisorBinary,
		binaryPath: "/usr/bin/cloud-hypervisor",
	}

	assert.True(t, ch.UsesKVM())
	assert.Equal(t, "/usr/bin/cloud-hypervisor", ch.Path())
	assert.NoError(t, ch.Ok())
	assert.NoError(t, ch.PreExec(types.ExecArgs{}))
}

func TestCloudHypervisorBuildExecCmd(t *testing.T) {
	t.Parallel()
	ch := &CloudHypervisor{
		binaryPath: "/usr/bin/cloud-hypervisor",
	}

	mockUk := &mockUnikernel{}

	t.Run("basic command", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: "/path/to/kernel",
			Command:       "console=ttyS0",
			MemSizeB:      256 * 1000 * 1000,
		}

		cmd, err := ch.BuildExecCmd(args, mockUk)
		require.NoError(t, err)
		assert.Equal(t, "/usr/bin/cloud-hypervisor", cmd[0])
		assert.Contains(t, cmd, "--memory")
		assert.Contains(t, cmd, "size=256M")
		assert.Contains(t, cmd, "--kernel")
		assert.Contains(t, cmd, "/path/to/kernel")
		assert.Contains(t, cmd, "--cmdline")
		assert.Contains(t, cmd, "console=ttyS0")
	})

	t.Run("virtiofs sharedfs", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: "/path/to/kernel",
			MemSizeB:      256 * 1000 * 1000,
			Sharedfs: types.SharedfsParams{
				Type: "virtiofs",
			},
		}

		cmd, err := ch.BuildExecCmd(args, mockUk)
		require.NoError(t, err)
		assert.Contains(t, cmd, "size=256M,shared=on")
		assert.Contains(t, cmd, "--fs")
		assert.Contains(t, cmd, "tag=fs0,socket=/tmp/vhostqemu")
	})

	t.Run("cpus and seccomp", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: "/path/to/kernel",
			VCPUs:         4,
			Seccomp:       true,
		}

		cmd, err := ch.BuildExecCmd(args, mockUk)
		require.NoError(t, err)
		assert.Contains(t, cmd, "--cpus")
		assert.Contains(t, cmd, "boot=4")
		assert.Contains(t, cmd, "--seccomp")
		assert.Contains(t, cmd, "true")
	})

	t.Run("network tap configuration", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: "/path/to/kernel",
			Net: types.NetDevParams{
				TapDev: "tap0_urunc",
				MAC:    "aa:bb:cc:dd:ee:ff",
				MTU:    1500,
			},
		}

		cmd, err := ch.BuildExecCmd(args, mockUk)
		require.NoError(t, err)
		assert.Contains(t, cmd, "--net")
		assert.Contains(t, cmd, "tap=tap0_urunc,mac=aa:bb:cc:dd:ee:ff,mtu=1500")
	})

	t.Run("initrd configuration", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: "/path/to/kernel",
			InitrdPath:    "/path/to/initramfs",
		}

		cmd, err := ch.BuildExecCmd(args, mockUk)
		require.NoError(t, err)
		assert.Contains(t, cmd, "--initramfs")
		assert.Contains(t, cmd, "/path/to/initramfs")
	})
}
