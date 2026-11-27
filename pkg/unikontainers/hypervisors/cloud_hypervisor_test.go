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
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

const testCHBinary = "/usr/bin/cloud-hypervisor"

// Disabling the arm64 serial device also prevents Cloud Hypervisor from
// appending an earlycon parameter after the guest's init arguments.
func TestCloudHypervisorBuildExecCmdConsole(t *testing.T) {
	t.Parallel()

	linux := &unikernels.Linux{
		Monitor:    "cloud-hypervisor",
		RootFsType: "initrd",
		App:        "/urunit",
		Command:    "/bin/echo 'hello world' --flag",
		InitrdConf: true,
	}
	cmdline, err := linux.CommandString()
	require.NoError(t, err)
	ch := &CloudHypervisor{binaryPath: testCHBinary}
	args, err := ch.BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		InitrdPath:    "/initrd.cpio",
		Command:       cmdline,
	}, linux)
	require.NoError(t, err)

	console, serial, guestConsole := "off", "tty", "console=ttyS0"
	if runtime.GOARCH == "arm64" {
		console, serial, guestConsole = "tty", "off", "console=hvc0"
	}
	for option, value := range map[string]string{
		"--console": console,
		"--serial":  serial,
		"--cmdline": cmdline,
	} {
		index := slices.Index(args, option)
		require.GreaterOrEqual(t, index, 0, "missing %s", option)
		require.Less(t, index+1, len(args))
		assert.NotContains(t, args[index+1:], option, "%s must only occur once", option)
		assert.Equal(t, value, args[index+1], option)
	}
	assert.Contains(t, strings.Fields(cmdline), guestConsole)
}

// TestCloudHypervisorBuildExecCmdSocket verifies Cloud Hypervisor emits
// --api-socket only when socket_path is set.
func TestCloudHypervisorBuildExecCmdSocket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		args           types.ExecArgs
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
			mustContain: []string{"--api-socket path=/run/urunc/ch.sock"},
		},
		{
			name: "unset SocketPath omits --api-socket",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				ContainerID:   "abc123",
			},
			mustNotContain: []string{"--api-socket"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: testCHBinary}
			out, err := ch.BuildExecCmd(tt.args, &fakeUnikernel{})
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
