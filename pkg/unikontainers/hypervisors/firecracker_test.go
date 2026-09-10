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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func newTestFirecracker() *Firecracker {
	return &Firecracker{
		binary:     FirecrackerBinary,
		binaryPath: "/usr/bin/firecracker",
	}
}

func TestFirecrackerBuildExecCmd(t *testing.T) {
	t.Parallel()

	t.Run("default fallback config path when ContainerID is empty", func(t *testing.T) {
		t.Parallel()
		fc := newTestFirecracker()
		args := types.ExecArgs{
			UnikernelPath: "/path/to/kernel",
			Command:       "console=ttyS0",
		}
		fakeUk := &fakeUnikernel{}

		exArgs, err := fc.BuildExecCmd(args, fakeUk)
		require.NoError(t, err)

		expectedConfigPath := filepath.Join("/tmp/", FCJsonFilename)
		assert.Equal(t, []string{
			"/usr/bin/firecracker",
			"--no-api",
			"--config-file",
			expectedConfigPath,
			"--no-seccomp",
		}, exArgs)

		// Clean up generated json config file
		_ = os.Remove(expectedConfigPath)
	})

	t.Run("container-isolated config path when ContainerID is set", func(t *testing.T) {
		t.Parallel()
		fc := newTestFirecracker()
		containerID := "test-container-12345"
		args := types.ExecArgs{
			ContainerID:   containerID,
			UnikernelPath: "/path/to/kernel",
			Command:       "console=ttyS0",
			Seccomp:       true,
		}
		fakeUk := &fakeUnikernel{}

		exArgs, err := fc.BuildExecCmd(args, fakeUk)
		require.NoError(t, err)

		expectedConfigPath := filepath.Join("/tmp/", "fc-"+containerID+".json")
		assert.Equal(t, []string{
			"/usr/bin/firecracker",
			"--no-api",
			"--config-file",
			expectedConfigPath,
		}, exArgs)

		// Verify file exists and clean up
		_, err = os.Stat(expectedConfigPath)
		assert.NoError(t, err, "isolated config file should be created")
		_ = os.Remove(expectedConfigPath)
	})
}
