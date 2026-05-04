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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func newTestHVT() *HVT {
	return &HVT{
		binary:     HvtBinary,
		binaryPath: "/usr/bin/solo5-hvt",
	}
}

func TestHVTUsesKVM(t *testing.T) {
	t.Parallel()
	assert.True(t, newTestHVT().UsesKVM())
}

func TestHVTSupportsSharedfs(t *testing.T) {
	t.Parallel()
	h := newTestHVT()
	assert.False(t, h.SupportsSharedfs("virtio"))
	assert.False(t, h.SupportsSharedfs("9p"))
	assert.False(t, h.SupportsSharedfs(""))
}

func TestHVTPath(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "/usr/bin/solo5-hvt", newTestHVT().Path())
}

func TestHVTOkBinaryNotFound(t *testing.T) {
	t.Parallel()
	assert.ErrorIs(t, newTestHVT().Ok(), ErrVMMNotInstalled)
}

func TestHVTPreExecNoSeccomp(t *testing.T) {
	t.Parallel()
	assert.NoError(t, newTestHVT().PreExec(types.ExecArgs{Seccomp: false}))
}

func TestHVTBuildExecCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        types.ExecArgs
		unikernel   *mockUnikernel
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "binary path is first element",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel.bin",
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"/usr/bin/solo5-hvt"},
		},
		{
			name: "default memory when MemSizeB is zero",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel.bin",
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--mem=256"},
		},
		{
			name: "memory set from MemSizeB",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel.bin",
				MemSizeB:      512 * 1000 * 1000,
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"--mem=512"},
		},
		{
			name: "net cli from unikernel when TapDev set",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel.bin",
				Net: types.NetDevParams{
					TapDev: "tap0",
					MAC:    "aa:bb:cc:dd:ee:ff",
				},
			},
			unikernel:   &mockUnikernel{netCli: "--net:service0=tap0"},
			wantContain: []string{"--net:service0=tap0"},
		},
		{
			name: "no net args when TapDev is empty",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel.bin",
			},
			unikernel:  &mockUnikernel{},
			wantAbsent: []string{"--net"},
		},
		{
			name: "block device appended",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel.bin",
			},
			unikernel: &mockUnikernel{
				blockCli: []types.MonitorBlockArgs{
					{ID: "storage0", Path: "/dev/vda"},
				},
			},
			wantContain: []string{"--block:storage0=/dev/vda"},
		},
		{
			name: "extra args from MonitorCli appended",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel.bin",
			},
			unikernel: &mockUnikernel{
				monCli: types.MonitorCliArgs{OtherArgs: "--dumpcore"},
			},
			wantContain: []string{"--dumpcore"},
		},
		{
			name: "unikernel path and command are last",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel.bin",
				Command:       "console=ttyS0",
			},
			unikernel:   &mockUnikernel{},
			wantContain: []string{"/unikernel.bin", "console=ttyS0"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newTestHVT()
			got, err := h.BuildExecCmd(tc.args, tc.unikernel)
			assert.NoError(t, err)
			assert.Equal(t, "/usr/bin/solo5-hvt", got[0])

			joined := strings.Join(got, " ")
			for _, want := range tc.wantContain {
				assert.Contains(t, joined, want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, joined, absent)
			}
		})
	}
}
