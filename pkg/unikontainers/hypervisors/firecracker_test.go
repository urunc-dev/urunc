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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

type mockUnikernel struct {
	netCli   string
	blockCli []types.MonitorBlockArgs
	monCli   types.MonitorCliArgs
}

func (m *mockUnikernel) Init(_ types.UnikernelParams) error        { return nil }
func (m *mockUnikernel) CommandString() (string, error)            { return "", nil }
func (m *mockUnikernel) SupportsBlock() bool                       { return false }
func (m *mockUnikernel) SupportsFS(_ string) bool                  { return false }
func (m *mockUnikernel) MonitorNetCli(_, _ string) string          { return m.netCli }
func (m *mockUnikernel) MonitorBlockCli() []types.MonitorBlockArgs { return m.blockCli }
func (m *mockUnikernel) MonitorCli() types.MonitorCliArgs          { return m.monCli }

func TestFirecrackerBuildExecCmd(t *testing.T) {
	t.Run("typical run", func(t *testing.T) {
		fc := &Firecracker{binary: FirecrackerBinary, binaryPath: "/usr/bin/firecracker"}

		uk := &mockUnikernel{
			blockCli: []types.MonitorBlockArgs{
				{ID: "rootfs", Path: "/dev/vda"},
				{ID: "extra", Path: "/dev/vdb"},
			},
		}
		cmd, err := fc.BuildExecCmd(types.ExecArgs{
			UnikernelPath: "/kernel",
			Command:       "console=ttyS0",
			MemSizeB:      512 * 1024 * 1024,
			VCPUs:         2,
			Net: types.NetDevParams{
				TapDev: "tap0",
				MAC:    "aa:bb:cc:dd:ee:ff",
			},
		}, uk)
		if !assert.NoError(t, err) {
			return
		}

		assert.Equal(t, []string{
			"/usr/bin/firecracker",
			"--no-api",
			"--config-file",
			"/tmp/fc.json",
			"--no-seccomp",
		}, cmd)

		raw, err := os.ReadFile(filepath.Join("/tmp", FCJsonFilename))
		if !assert.NoError(t, err) {
			return
		}

		var cfg FirecrackerConfig
		if !assert.NoError(t, json.Unmarshal(raw, &cfg)) {
			return
		}

		assert.Equal(t, "/kernel", cfg.Source.ImagePath)
		assert.Equal(t, "console=ttyS0", cfg.Source.BootArgs)

		assert.Equal(t, uint(2), cfg.Machine.VcpuCount)
		assert.Equal(t, uint64(512), cfg.Machine.MemSizeMiB)
		assert.False(t, cfg.Machine.Smt)

		if assert.Len(t, cfg.NetIfs, 1) {
			assert.Equal(t, "net1", cfg.NetIfs[0].IfaceID)
			assert.Equal(t, "tap0", cfg.NetIfs[0].HostIF)
			assert.Equal(t, "aa:bb:cc:dd:ee:ff", cfg.NetIfs[0].GuestMAC)
		}

		if assert.Len(t, cfg.Drives, 2) {
			assert.True(t, cfg.Drives[0].IsRootDev, "rootfs should be flagged root")
			assert.False(t, cfg.Drives[1].IsRootDev)
		}
	})

	t.Run("seccomp flag", func(t *testing.T) {
		fc := &Firecracker{binaryPath: "/usr/bin/firecracker"}

		off, err := fc.BuildExecCmd(types.ExecArgs{UnikernelPath: "/kernel"}, &mockUnikernel{})
		assert.NoError(t, err)
		assert.Contains(t, off, "--no-seccomp")

		on, err := fc.BuildExecCmd(types.ExecArgs{UnikernelPath: "/kernel", Seccomp: true}, &mockUnikernel{})
		assert.NoError(t, err)
		assert.NotContains(t, on, "--no-seccomp")
	})

	t.Run("memory fallback", func(t *testing.T) {
		fc := &Firecracker{binaryPath: "/usr/bin/firecracker"}

		for _, b := range []uint64{0, 1024, 1024 * 1024 * 0} {
			_, err := fc.BuildExecCmd(types.ExecArgs{
				UnikernelPath: "/kernel",
				MemSizeB:      b,
			}, &mockUnikernel{})
			assert.NoError(t, err)

			raw, _ := os.ReadFile("/tmp/fc.json")
			var cfg FirecrackerConfig
			assert.NoError(t, json.Unmarshal(raw, &cfg))
			assert.Equal(t, DefaultMemory, cfg.Machine.MemSizeMiB,
				"sub-MiB or zero MemSizeB should give default, got bytes=%d", b)
		}
	})

	t.Run("initrd priority", func(t *testing.T) {
		fc := &Firecracker{binaryPath: "/usr/bin/firecracker"}

		_, err := fc.BuildExecCmd(types.ExecArgs{
			UnikernelPath: "/kernel",
			InitrdPath:    "/from-args.img",
		}, &mockUnikernel{
			monCli: types.MonitorCliArgs{ExtraInitrd: "/from-monitor.img"},
		})
		assert.NoError(t, err)

		raw, _ := os.ReadFile("/tmp/fc.json")
		var cfg FirecrackerConfig
		assert.NoError(t, json.Unmarshal(raw, &cfg))
		assert.Equal(t, "/from-args.img", cfg.Source.InitrdPath)

		_, err = fc.BuildExecCmd(types.ExecArgs{UnikernelPath: "/kernel"}, &mockUnikernel{
			monCli: types.MonitorCliArgs{ExtraInitrd: "/from-monitor.img"},
		})
		assert.NoError(t, err)

		raw, _ = os.ReadFile("/tmp/fc.json")
		assert.NoError(t, json.Unmarshal(raw, &cfg))
		assert.Equal(t, "/from-monitor.img", cfg.Source.InitrdPath)
	})

	t.Run("vsock", func(t *testing.T) {
		fc := &Firecracker{binaryPath: "/usr/bin/firecracker"}

		_, err := fc.BuildExecCmd(types.ExecArgs{
			UnikernelPath: "/kernel",
			VAccelType:    "vsock",
			VSockDevID:    7,
			VSockDevPath:  "/run/vaccel",
		}, &mockUnikernel{})
		assert.NoError(t, err)

		raw, _ := os.ReadFile("/tmp/fc.json")
		var cfg FirecrackerConfig
		assert.NoError(t, json.Unmarshal(raw, &cfg))
		assert.Equal(t, 7, cfg.VSock.GuestCID)
		assert.Equal(t, "/run/vaccel/vaccel.sock", cfg.VSock.UDSPath)
		assert.Equal(t, "root", cfg.VSock.VSockID)
	})
}
