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
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestCloudHypervisorMultiQueueNet(t *testing.T) {
	ch := &CloudHypervisor{binaryPath: "/usr/bin/cloud-hypervisor", binary: CloudHypervisorBinary}
	args := types.ExecArgs{
		UnikernelPath: "/path/to/kernel",
		Net: types.NetDevParams{
			TapDev: "tap0",
			MAC:    "52:54:00:12:34:56",
			MTU:    1500,
			Queues: 4,
		},
	}

	execCmd, err := ch.BuildExecCmd(args, &fakeUnikernel{})
	assert.NoError(t, err)

	foundNet := false
	for i, arg := range execCmd {
		if arg == "--net" && i+1 < len(execCmd) {
			netVal := execCmd[i+1]
			assert.Contains(t, netVal, "tap=tap0")
			assert.Contains(t, netVal, "mac=52:54:00:12:34:56")
			assert.Contains(t, netVal, "mtu=1500")
			assert.Contains(t, netVal, "num_queues=4")
			foundNet = true
			break
		}
	}
	assert.True(t, foundNet, "--net argument should be present")
}
