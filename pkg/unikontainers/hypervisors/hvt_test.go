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

func TestHVTBuildExecCmd(t *testing.T) {
	t.Parallel()

	h := &HVT{
		binaryPath: "/usr/local/bin/solo5-hvt",
		binary:     "solo5-hvt",
	}

	tests := []struct {
		name        string
		args        types.ExecArgs
		unikernel   types.Unikernel
		mustContain []string
	}{
		{
			name: "defaults render default memory",
			args: types.ExecArgs{
				UnikernelPath: "/path/to/unikernel",
				Command:       "hello",
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"/usr/local/bin/solo5-hvt",
				"--mem=256",
				"/path/to/unikernel",
				"hello",
			},
		},
		{
			name: "custom memory above 1 MiB",
			args: types.ExecArgs{
				UnikernelPath: "/path/to/unikernel",
				Command:       "hello",
				MemSizeB:      16 * 1024 * 1024, // 16 MiB
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"--mem=16",
			},
		},
		{
			name: "custom memory exactly 1 MiB",
			args: types.ExecArgs{
				UnikernelPath: "/path/to/unikernel",
				Command:       "hello",
				MemSizeB:      1024 * 1024, // 1 MiB
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"--mem=1",
			},
		},
		{
			name: "custom memory sub-1-MiB clamps to 1 MiB",
			args: types.ExecArgs{
				UnikernelPath: "/path/to/unikernel",
				Command:       "hello",
				MemSizeB:      512 * 1024, // 512 KiB
			},
			unikernel: &fakeUnikernel{},
			mustContain: []string{
				"--mem=1",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmdArgs, err := h.BuildExecCmd(tc.args, tc.unikernel)
			assert.NoError(t, err)
			for _, expected := range tc.mustContain {
				assert.Contains(t, cmdArgs, expected)
			}
		})
	}
}
