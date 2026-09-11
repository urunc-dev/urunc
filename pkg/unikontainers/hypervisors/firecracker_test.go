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

const (
	testFCBinary     = "/usr/bin/firecracker"
	testFCKernelPath = "/rootfs/unikernel.bin"
	testFCCommand    = "init=/bin/sh"
)

func TestFirecrackerBuildExecCmd(t *testing.T) {

	tests := []struct {
		name          string
		args          types.ExecArgs
		unikernel     types.Unikernel
		checkJSON     func(t *testing.T, config FirecrackerConfig)
		mustContain   []string // substrings in the joined command
		wantNoSeccomp bool     // expect --no-seccomp in the command
	}{
		{
			name: "baseline defaults with no network",
			args: types.ExecArgs{
				UnikernelPath: testFCKernelPath,
				Command:       testFCCommand,
				Seccomp:       true,
			},
			unikernel: &fakeUnikernel{},
			checkJSON: func(t *testing.T, config FirecrackerConfig) {
				assert.Equal(t, testFCKernelPath, config.Source.ImagePath)
				assert.Equal(t, testFCCommand, config.Source.BootArgs)
				assert.Equal(t, uint64(DefaultMemory), config.Machine.MemSizeMiB)
				assert.Empty(t, config.NetIfs, "no network interfaces expected")
			},
		},
		{
			name: "network interface includes MTU",
			args: types.ExecArgs{
				UnikernelPath: testFCKernelPath,
				Command:       testFCCommand,
				Seccomp:       true,
				Net: types.NetDevParams{
					TapDev: "tap0_urunc",
					MAC:    "52:54:00:12:34:56",
					MTU:    9000,
				},
			},
			unikernel: &fakeUnikernel{},
			checkJSON: func(t *testing.T, config FirecrackerConfig) {
				require.Len(t, config.NetIfs, 1)
				netIf := config.NetIfs[0]
				assert.Equal(t, "net1", netIf.IfaceID)
				assert.Equal(t, "52:54:00:12:34:56", netIf.GuestMAC)
				assert.Equal(t, "tap0_urunc", netIf.HostIF)
				assert.Equal(t, 9000, netIf.MTU, "MTU must be propagated to FC config")
			},
		},
		{
			name: "network interface with default MTU 1500",
			args: types.ExecArgs{
				UnikernelPath: testFCKernelPath,
				Command:       testFCCommand,
				Seccomp:       true,
				Net: types.NetDevParams{
					TapDev: "tap0_urunc",
					MAC:    "aa:bb:cc:dd:ee:ff",
					MTU:    1500,
				},
			},
			unikernel: &fakeUnikernel{},
			checkJSON: func(t *testing.T, config FirecrackerConfig) {
				require.Len(t, config.NetIfs, 1)
				assert.Equal(t, 1500, config.NetIfs[0].MTU)
			},
		},
		{
			name: "MTU zero is omitted from JSON",
			args: types.ExecArgs{
				UnikernelPath: testFCKernelPath,
				Command:       testFCCommand,
				Seccomp:       true,
				Net: types.NetDevParams{
					TapDev: "tap0_urunc",
					MAC:    "aa:bb:cc:dd:ee:ff",
					MTU:    0,
				},
			},
			unikernel: &fakeUnikernel{},
			checkJSON: func(t *testing.T, config FirecrackerConfig) {
				require.Len(t, config.NetIfs, 1)
				assert.Equal(t, 0, config.NetIfs[0].MTU)
			},
		},
		{
			name: "seccomp disabled emits --no-seccomp",
			args: types.ExecArgs{
				UnikernelPath: testFCKernelPath,
				Command:       testFCCommand,
				Seccomp:       false,
			},
			unikernel:     &fakeUnikernel{},
			wantNoSeccomp: true,
		},
		{
			name: "custom memory size",
			args: types.ExecArgs{
				UnikernelPath: testFCKernelPath,
				Command:       testFCCommand,
				Seccomp:       true,
				MemSizeB:      512 * 1024 * 1024,
			},
			unikernel: &fakeUnikernel{},
			checkJSON: func(t *testing.T, config FirecrackerConfig) {
				assert.Equal(t, uint64(512), config.Machine.MemSizeMiB)
			},
		},
		{
			name: "VCPUs are set",
			args: types.ExecArgs{
				UnikernelPath: testFCKernelPath,
				Command:       testFCCommand,
				Seccomp:       true,
				VCPUs:         4,
			},
			unikernel: &fakeUnikernel{},
			checkJSON: func(t *testing.T, config FirecrackerConfig) {
				assert.Equal(t, uint(4), config.Machine.VcpuCount)
			},
		},
		{
			name: "initrd path is set in boot source",
			args: types.ExecArgs{
				UnikernelPath: testFCKernelPath,
				Command:       testFCCommand,
				Seccomp:       true,
				InitrdPath:    "/rootfs/initrd.img",
			},
			unikernel: &fakeUnikernel{},
			checkJSON: func(t *testing.T, config FirecrackerConfig) {
				assert.Equal(t, "/rootfs/initrd.img", config.Source.InitrdPath)
			},
		},
		{
			name: "vsock device is configured",
			args: types.ExecArgs{
				UnikernelPath: testFCKernelPath,
				Command:       testFCCommand,
				Seccomp:       true,
				VAccelType:    "vsock",
				VSockDevID:    42,
				VSockDevPath:  "/run/vsock",
			},
			unikernel: &fakeUnikernel{},
			checkJSON: func(t *testing.T, config FirecrackerConfig) {
				assert.Equal(t, 42, config.VSock.GuestCID)
				assert.Equal(t, "/run/vsock/vaccel.sock", config.VSock.UDSPath)
				assert.Equal(t, "root", config.VSock.VSockID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Subtests cannot be parallel: they all write to /tmp/fc.json.
			os.Remove(filepath.Join("/tmp/", FCJsonFilename)) // clean slate

			fc := &Firecracker{binary: FirecrackerBinary, binaryPath: testFCBinary}
			out, err := fc.BuildExecCmd(tt.args, tt.unikernel)
			require.NoError(t, err)
			require.NotEmpty(t, out)

			// The first element must be the binary path.
			assert.Equal(t, testFCBinary, out[0], "binary path must be the first element")

			// Check --no-api and --config-file are always present.
			joined := ""
			for _, arg := range out {
				joined += arg + " "
			}
			assert.Contains(t, joined, "--no-api")
			assert.Contains(t, joined, "--config-file")

			if tt.wantNoSeccomp {
				assert.Contains(t, joined, "--no-seccomp")
			} else {
				assert.NotContains(t, joined, "--no-seccomp")
			}

			for _, want := range tt.mustContain {
				assert.Contains(t, joined, want, "expected %q to be present", want)
			}

			// Read back the generated JSON config and validate it.
			if tt.checkJSON != nil {
				jsonFile := filepath.Join("/tmp/", FCJsonFilename)
				data, err := os.ReadFile(jsonFile)
				require.NoError(t, err, "fc.json must be readable")

				var config FirecrackerConfig
				err = json.Unmarshal(data, &config)
				require.NoError(t, err, "fc.json must be valid JSON")

				tt.checkJSON(t, config)
			}
		})
	}
}

// TestFirecrackerNetMTUJSON verifies the JSON serialization of FirecrackerNet
// includes the "mtu" field when it is non-zero and omits it when zero.
func TestFirecrackerNetMTUJSON(t *testing.T) {
	t.Parallel()

	t.Run("non-zero MTU is serialized", func(t *testing.T) {
		t.Parallel()
		net := FirecrackerNet{
			IfaceID:  "net1",
			GuestMAC: "52:54:00:12:34:56",
			HostIF:   "tap0_urunc",
			MTU:      9000,
		}
		data, err := json.Marshal(net)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"mtu":9000`)
	})

	t.Run("zero MTU is omitted from JSON", func(t *testing.T) {
		t.Parallel()
		net := FirecrackerNet{
			IfaceID:  "net1",
			GuestMAC: "52:54:00:12:34:56",
			HostIF:   "tap0_urunc",
			MTU:      0,
		}
		data, err := json.Marshal(net)
		require.NoError(t, err)
		assert.NotContains(t, string(data), `"mtu"`, "zero MTU should be omitted via omitempty")
	})
}
