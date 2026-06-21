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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const testFcBinary = "/usr/bin/firecracker"

// runBuildExecCmd runs BuildExecCmd and reads back the json config it writes.
// Subtests must not run in parallel since the config path (/tmp/fc.json) is fixed.
func runBuildExecCmd(t *testing.T, args types.ExecArgs, ukernel types.Unikernel) ([]string, FirecrackerConfig) {
	t.Helper()

	fc := &Firecracker{binary: FirecrackerBinary, binaryPath: testFcBinary}
	out, err := fc.BuildExecCmd(args, ukernel)
	require.NoError(t, err)
	require.NotEmpty(t, out)

	jsonPath := filepath.Join("/tmp/", FCJsonFilename)
	data, err := os.ReadFile(jsonPath) //nolint: gosec
	require.NoError(t, err, "expected BuildExecCmd to write %s", jsonPath)

	var cfg FirecrackerConfig
	require.NoError(t, json.Unmarshal(data, &cfg))

	return out, cfg
}

func TestFirecrackerBuildExecCmdArgs(t *testing.T) {
	tests := []struct {
		name           string
		args           types.ExecArgs
		mustContain    []string
		mustNotContain []string
	}{
		{
			name: "defaults render no-api, config-file and no-seccomp",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			mustContain: []string{"--no-api", "--config-file", "/tmp/" + FCJsonFilename, "--no-seccomp"},
		},
		{
			name: "Seccomp=true omits the --no-seccomp flag",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				Seccomp:       true,
			},
			mustContain:    []string{"--no-api", "--config-file"},
			mustNotContain: []string{"--no-seccomp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := runBuildExecCmd(t, tt.args, &fakeUnikernel{})

			assert.Equal(t, testFcBinary, out[0], "binary path must be the first element")
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

func TestFirecrackerBuildExecCmdConfig(t *testing.T) {
	t.Run("defaults render baseline machine and boot source", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: testKernelPath,
			Command:       testCommand,
		}
		_, cfg := runBuildExecCmd(t, args, &fakeUnikernel{})

		assert.Equal(t, DefaultMemory, cfg.Machine.MemSizeMiB)
		assert.False(t, cfg.Machine.Smt)
		assert.False(t, cfg.Machine.TrackDirtyPages)

		assert.Equal(t, testKernelPath, cfg.Source.ImagePath)
		assert.Equal(t, testCommand, cfg.Source.BootArgs)
		assert.Empty(t, cfg.Source.InitrdPath)

		assert.Empty(t, cfg.NetIfs)
		assert.Empty(t, cfg.Drives)
	})

	t.Run("custom MemSizeB is converted to MiB", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: testKernelPath,
			Command:       testCommand,
			MemSizeB:      512 * 1024 * 1024,
		}
		_, cfg := runBuildExecCmd(t, args, &fakeUnikernel{})
		assert.Equal(t, uint64(512), cfg.Machine.MemSizeMiB)
	})

	t.Run("sub-MiB MemSizeB falls back to DefaultMemory", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: testKernelPath,
			Command:       testCommand,
			MemSizeB:      1024,
		}
		_, cfg := runBuildExecCmd(t, args, &fakeUnikernel{})
		assert.Equal(t, DefaultMemory, cfg.Machine.MemSizeMiB)
	})

	t.Run("VCPUs is propagated to the machine config", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: testKernelPath,
			Command:       testCommand,
			VCPUs:         4,
		}
		_, cfg := runBuildExecCmd(t, args, &fakeUnikernel{})
		assert.Equal(t, uint(4), cfg.Machine.VcpuCount)
	})

	t.Run("tap device renders a network interface", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: testKernelPath,
			Command:       testCommand,
			Net:           types.NetDevParams{TapDev: "tap0", MAC: "52:54:00:12:34:56"},
		}
		_, cfg := runBuildExecCmd(t, args, &fakeUnikernel{})
		require.Len(t, cfg.NetIfs, 1)
		assert.Equal(t, "net1", cfg.NetIfs[0].IfaceID)
		assert.Equal(t, "tap0", cfg.NetIfs[0].HostIF)
		assert.Equal(t, "52:54:00:12:34:56", cfg.NetIfs[0].GuestMAC)
	})

	t.Run("InitrdPath from args is used in the boot source", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: testKernelPath,
			Command:       testCommand,
			InitrdPath:    "/rootfs/initrd.img",
		}
		_, cfg := runBuildExecCmd(t, args, &fakeUnikernel{})
		assert.Equal(t, "/rootfs/initrd.img", cfg.Source.InitrdPath)
	})

	t.Run("InitrdPath falls back to MonitorCli ExtraInitrd", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: testKernelPath,
			Command:       testCommand,
		}
		uk := &fakeUnikernel{monitorCli: types.MonitorCliArgs{ExtraInitrd: "/extra/initrd.cpio"}}
		_, cfg := runBuildExecCmd(t, args, uk)
		assert.Equal(t, "/extra/initrd.cpio", cfg.Source.InitrdPath)
	})

	t.Run("block devices render drives and mark rootfs as root device", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: testKernelPath,
			Command:       testCommand,
		}
		uk := &fakeUnikernel{blockCli: []types.MonitorBlockArgs{
			{ID: "rootfs", Path: "/disks/root.img"},
			{ID: "data0", Path: "/disks/data0.img"},
		}}
		_, cfg := runBuildExecCmd(t, args, uk)
		require.Len(t, cfg.Drives, 2)
		assert.Equal(t, "rootfs", cfg.Drives[0].DriveID)
		assert.Equal(t, "/disks/root.img", cfg.Drives[0].HostPath)
		assert.True(t, cfg.Drives[0].IsRootDev, "rootfs drive must be the root device")
		assert.Equal(t, "data0", cfg.Drives[1].DriveID)
		assert.False(t, cfg.Drives[1].IsRootDev, "non-rootfs drive must not be the root device")
	})

	t.Run("vsock vAccel renders the vsock device with the guest CID", func(t *testing.T) {
		args := types.ExecArgs{
			UnikernelPath: testKernelPath,
			Command:       testCommand,
			VAccelType:    "vsock",
			VSockDevID:    42,
			VSockDevPath:  "/run/vaccel",
		}
		_, cfg := runBuildExecCmd(t, args, &fakeUnikernel{})
		assert.Equal(t, 42, cfg.VSock.GuestCID)
		assert.Equal(t, "root", cfg.VSock.VSockID)
		assert.Equal(t, "/run/vaccel/vaccel.sock", cfg.VSock.UDSPath)
	})
}

func TestFirecrackerSimpleMethods(t *testing.T) {
	fc := &Firecracker{binary: FirecrackerBinary, binaryPath: testFcBinary}

	assert.Equal(t, testFcBinary, fc.Path())
	assert.True(t, fc.UsesKVM())
	assert.NoError(t, fc.Ok())
	assert.NoError(t, fc.PreExec(types.ExecArgs{}))
	assert.False(t, fc.SupportsSharedfs("9pfs"))
	assert.False(t, fc.SupportsSharedfs("virtiofs"))
}
