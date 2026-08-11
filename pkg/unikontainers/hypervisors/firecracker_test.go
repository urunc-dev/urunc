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
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// mockUnikernel is a mock implementation of the Unikernel interface for testing
type mockUnikernel struct {
	cliArgs    types.MonitorCliArgs
	blockCli   []types.MonitorBlockArgs
	netCli     string
	vmmMetrics string
}

func (m *mockUnikernel) MonitorCli() types.MonitorCliArgs         { return m.cliArgs }
func (m *mockUnikernel) MonitorBlockCli() []types.MonitorBlockArgs { return m.blockCli }
func (m *mockUnikernel) MonitorNetCli(string, string) string       { return m.netCli }
func (m *mockUnikernel) VmmMetrics() string                        { return m.vmmMetrics }
func (m *mockUnikernel) Init(types.UnikernelParams) error          { return nil }
func (m *mockUnikernel) CommandString() (string, error)            { return "", nil }
func (m *mockUnikernel) SupportsBlock() bool                       { return false }
func (m *mockUnikernel) SupportsFS(string) bool                    { return false }

func TestFirecrackerBuildExecCmd(t *testing.T) {
	// We cannot run this safely in parallel if it overwrites a hardcoded file in /tmp/fc.json
	// So t.Parallel() is intentionally omitted.

	fc := &Firecracker{
		binaryPath: "/usr/bin/firecracker",
		binary:     "firecracker",
	}
	expectedConfigFile := filepath.Join("/tmp/", FCJsonFilename)

	testCases := []struct {
		name         string
		args         types.ExecArgs
		ukernel      types.Unikernel
		expectedCmd  []string
		expectedJSON FirecrackerConfig
	}{
		{
			name: "Basic configuration without network, seccomp enabled",
			args: types.ExecArgs{
				Seccomp:       true,
				VCPUs:         2,
				MemSizeB:      1024 * 1024 * 512, // 512 MiB
				UnikernelPath: "/path/to/unikernel",
				Command:       "hello world",
			},
			ukernel: &mockUnikernel{},
			expectedCmd: []string{
				"/usr/bin/firecracker",
				"--no-api",
				"--config-file",
				expectedConfigFile,
			},
			expectedJSON: FirecrackerConfig{
				Source: FirecrackerBootSource{
					ImagePath: "/path/to/unikernel",
					BootArgs:  "hello world",
				},
				Machine: FirecrackerMachine{
					VcpuCount:  2,
					MemSizeMiB: 512,
				},
				Drives: []FirecrackerDrive{},
				NetIfs: []FirecrackerNet{},
			},
		},
		{
			name: "Configuration with seccomp disabled, initrd, network, block and vsock",
			args: types.ExecArgs{
				Seccomp:       false,
				VCPUs:         4,
				MemSizeB:      1024 * 1024 * 2048, // 2048 MiB
				UnikernelPath: "/path/to/unikernel",
				Command:       "hello world",
				InitrdPath:    "/path/to/initrd",
				Net: types.TapDevice{
					TapDev: "tap0",
					MAC:    "00:11:22:33:44:55",
				},
				VAccelType:   "vsock",
				VSockDevPath: "/var/run/vsock",
				VSockDevID:   3,
			},
			ukernel: &mockUnikernel{
				blockCli: []types.MonitorBlockArgs{
					{ID: "rootfs", Path: "/path/to/rootfs"},
					{ID: "data", Path: "/path/to/data"},
				},
			},
			expectedCmd: []string{
				"/usr/bin/firecracker",
				"--no-api",
				"--config-file",
				expectedConfigFile,
				"--no-seccomp",
			},
			expectedJSON: FirecrackerConfig{
				Source: FirecrackerBootSource{
					ImagePath:  "/path/to/unikernel",
					BootArgs:   "hello world",
					InitrdPath: "/path/to/initrd",
				},
				Machine: FirecrackerMachine{
					VcpuCount:  4,
					MemSizeMiB: 2048,
				},
				NetIfs: []FirecrackerNet{
					{IfaceID: "net1", GuestMAC: "00:11:22:33:44:55", HostIF: "tap0"},
				},
				Drives: []FirecrackerDrive{
					{DriveID: "rootfs", IsRO: false, IsRootDev: true, HostPath: "/path/to/rootfs"},
					{DriveID: "data", IsRO: false, IsRootDev: false, HostPath: "/path/to/data"},
				},
				VSock: FirecrackerVSockDev{
					GuestCID: 3,
					UDSPath:  "/var/run/vsock/vaccel.sock",
					VSockID:  "root",
				},
			},
		},
		{
			name: "Initrd from Unikernel.MonitorCli priority",
			args: types.ExecArgs{
				Seccomp:       true,
				VCPUs:         1,
				MemSizeB:      1024 * 1024 * 128,
				UnikernelPath: "/path/to/unikernel",
				InitrdPath:    "", // Empty, so it should fall back to ExtraInitrd
			},
			ukernel: &mockUnikernel{
				cliArgs: types.MonitorCliArgs{
					ExtraInitrd: "/path/to/extra_initrd",
				},
			},
			expectedCmd: []string{
				"/usr/bin/firecracker",
				"--no-api",
				"--config-file",
				expectedConfigFile,
			},
			expectedJSON: FirecrackerConfig{
				Source: FirecrackerBootSource{
					ImagePath:  "/path/to/unikernel",
					InitrdPath: "/path/to/extra_initrd",
				},
				Machine: FirecrackerMachine{
					VcpuCount:  1,
					MemSizeMiB: 128,
				},
				Drives: []FirecrackerDrive{},
				NetIfs: []FirecrackerNet{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean up any existing config file
			_ = os.Remove(expectedConfigFile)

			cmd, err := fc.BuildExecCmd(tc.args, tc.ukernel)
			require.NoError(t, err)

			assert.Equal(t, tc.expectedCmd, cmd)

			// Read and verify the generated JSON configuration
			fileBytes, err := os.ReadFile(expectedConfigFile)
			require.NoError(t, err, "Config file should be created")

			var actualJSON FirecrackerConfig
			err = json.Unmarshal(fileBytes, &actualJSON)
			require.NoError(t, err, "Config file should be valid JSON")

			assert.Equal(t, tc.expectedJSON.Source, actualJSON.Source)
			assert.Equal(t, tc.expectedJSON.Machine, actualJSON.Machine)
			assert.Equal(t, tc.expectedJSON.NetIfs, actualJSON.NetIfs)
			assert.Equal(t, tc.expectedJSON.Drives, actualJSON.Drives)
			assert.Equal(t, tc.expectedJSON.VSock, actualJSON.VSock)
		})
	}

	// Clean up after the test suite
	_ = os.Remove(expectedConfigFile)
}

func TestFirecrackerProperties(t *testing.T) {
	t.Parallel()
	fc := &Firecracker{binaryPath: "/bin/fc"}

	assert.Equal(t, "/bin/fc", fc.Path())
	assert.True(t, fc.UsesKVM())
	assert.False(t, fc.SupportsSharedfs("virtiofs"))
	assert.NoError(t, fc.Ok())
	assert.NoError(t, fc.PreExec(types.ExecArgs{}))
}
