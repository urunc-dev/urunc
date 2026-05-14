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

type mockUnikernel struct {
	extraInitrd string
}

func (m *mockUnikernel) Init(_ types.UnikernelParams) error        { return nil }
func (m *mockUnikernel) CommandString() (string, error)            { return "", nil }
func (m *mockUnikernel) SupportsBlock() bool                       { return false }
func (m *mockUnikernel) SupportsFS(_ string) bool                  { return false }
func (m *mockUnikernel) MonitorNetCli(_, _ string) string          { return "" }
func (m *mockUnikernel) MonitorBlockCli() []types.MonitorBlockArgs { return nil }
func (m *mockUnikernel) MonitorCli() types.MonitorCliArgs {
	return types.MonitorCliArgs{ExtraInitrd: m.extraInitrd}
}

func TestCloudHypervisorSingleInitrd(t *testing.T) {
	t.Parallel()
	ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: "/usr/bin/cloud-hypervisor"}
	args := types.ExecArgs{
		UnikernelPath: "/unikernel",
		InitrdPath:    "/primary/initrd",
		MemSizeB:      256 * 1024 * 1024,
		Command:       "console=ttyS0",
	}
	ukernel := &mockUnikernel{extraInitrd: "/extra/initrd"}

	result, err := ch.BuildExecCmd(args, ukernel)
	assert.NoError(t, err)

	count := 0
	for _, arg := range result {
		if arg == "--initramfs" {
			count++
		}
	}
	assert.Equal(t, 1, count, "expected exactly one --initramfs flag")
}
