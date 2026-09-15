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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const testFCBinary = "/usr/bin/firecracker"

// configPathFromArgs pulls the JSON config path out of the args returned by
// BuildExecCmd (the element right after "--config-file").
func configPathFromArgs(t *testing.T, args []string) string {
	t.Helper()
	for i, a := range args {
		if a == "--config-file" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("no --config-file argument in %v", args)
	return ""
}

// TestFirecrackerBuildExecCmdVSock verifies that the generated Firecracker
// config only contains a "vsock" device when vAccel/vsock is requested. The
// field is tagged omitempty, so without vAccel the key must be absent.
func TestFirecrackerBuildExecCmdVSock(t *testing.T) {
	// Not parallel: BuildExecCmd writes to a fixed /tmp path.
	tests := []struct {
		name      string
		args      types.ExecArgs
		wantVSock bool
	}{
		{
			name: "no vAccel omits the vsock device",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
			},
			wantVSock: false,
		},
		{
			name: "vAccel vsock includes the vsock device",
			args: types.ExecArgs{
				UnikernelPath: testKernelPath,
				Command:       testCommand,
				VAccelType:    "vsock",
				VSockDevID:    42,
				VSockDevPath:  "/run/vaccel",
			},
			wantVSock: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &Firecracker{binary: FirecrackerBinary, binaryPath: testFCBinary}
			out, err := fc.BuildExecCmd(tt.args, &fakeUnikernel{})
			assert.NoError(t, err)

			cfgPath := configPathFromArgs(t, out)
			t.Cleanup(func() { _ = os.Remove(cfgPath) })

			data, err := os.ReadFile(cfgPath)
			assert.NoError(t, err)

			var cfg map[string]json.RawMessage
			assert.NoError(t, json.Unmarshal(data, &cfg))

			_, present := cfg["vsock"]
			assert.Equal(t, tt.wantVSock, present,
				"unexpected vsock presence in generated config: %s", string(data))

			if tt.wantVSock {
				assert.Contains(t, string(cfg["vsock"]), `"guest_cid":42`,
					"vsock device should carry the configured guest CID")
			}
		})
	}
}
