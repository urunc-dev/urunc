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
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// The two solo5 tenders (hvt and spt) share the same cli grammar:
// "[ OPTIONS ] [ -- ] KERNEL [ ARGS ]". The runners below exercise that shared
// behaviour and are called with the concrete tender from hvt_test.go and
// spt_test.go.
const (
	testHvtBinary = "/usr/bin/solo5-hvt"
	testSptBinary = "/usr/bin/solo5-spt"
)

type solo5Case struct {
	name           string
	args           types.ExecArgs
	unikernel      types.Unikernel
	mustContain    []string
	mustNotContain []string
}

func runSolo5BuildExecCmd(t *testing.T, vmm types.VMM) {
	t.Helper()

	tests := []solo5Case{
		{
			name:           "defaults render the baseline solo5 command",
			args:           types.ExecArgs{UnikernelPath: testKernelPath},
			unikernel:      &fakeUnikernel{},
			mustContain:    []string{"--mem=256", testKernelPath},
			mustNotContain: []string{"--net:", "--block:"},
		},
		{
			name:        "custom MemSizeB renders --mem in MB",
			args:        types.ExecArgs{UnikernelPath: testKernelPath, MemSizeB: 512 * 1000 * 1000},
			unikernel:   &fakeUnikernel{},
			mustContain: []string{"--mem=512"},
		},
		{
			name:        "MonitorNetCli is used verbatim when a tap device is set",
			args:        types.ExecArgs{UnikernelPath: testKernelPath, Net: types.NetDevParams{TapDev: "tap0"}},
			unikernel:   &fakeUnikernel{netCli: []string{"--net:service=tap0"}},
			mustContain: []string{"--net:service=tap0"},
		},
		{
			name: "block devices render --block:ID=PATH and skip empty paths",
			args: types.ExecArgs{UnikernelPath: testKernelPath},
			unikernel: &fakeUnikernel{blockCli: []types.MonitorBlockArgs{
				{ID: "data0", Path: "/disks/data0.img"},
				{ID: "empty", Path: ""},
			}},
			mustContain:    []string{"--block:data0=/disks/data0.img"},
			mustNotContain: []string{"--block:empty="},
		},
		{
			name:        "MonitorCli OtherArgs are appended verbatim",
			args:        types.ExecArgs{UnikernelPath: testKernelPath},
			unikernel:   &fakeUnikernel{monitorCli: types.MonitorCliArgs{OtherArgs: []string{"--block-sector-size:data0=512"}}},
			mustContain: []string{"--block-sector-size:data0=512"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := vmm.BuildExecCmd(tt.args, tt.unikernel)
			assert.NoError(t, err)
			assert.NotEmpty(t, got)

			// Invariants that hold for every solo5 command.
			assert.Equal(t, vmm.Path(), got[0], "binary path must be the first element")
			sepIdx := slices.Index(got, solo5ArgsSeparator)
			if sepIdx == -1 || sepIdx+1 >= len(got) {
				t.Fatalf("%q must be present and followed by the kernel path, got %v", solo5ArgsSeparator, got)
			}
			assert.Equal(t, tt.args.UnikernelPath, got[sepIdx+1],
				"the kernel path must directly follow %q", solo5ArgsSeparator)

			joined := strings.Join(got, " ")
			for _, want := range tt.mustContain {
				assert.Contains(t, joined, want, "expected %q to be present", want)
			}
			for _, notWant := range tt.mustNotContain {
				assert.NotContains(t, joined, notWant, "expected %q to be absent", notWant)
			}
		})
	}
}

func runSolo5UnikernelPathOneElement(t *testing.T, vmm types.VMM) {
	t.Helper()

	annotationValue := "/unikernel/app.bin --block:extra=/a/b --mem=1 --net:service=tap0"
	got, err := vmm.BuildExecCmd(types.ExecArgs{
		UnikernelPath: annotationValue,
		MemSizeB:      256 * 1024 * 1024,
	}, &fakeUnikernel{})
	assert.NoError(t, err)

	assert.Contains(t, got, annotationValue, "the annotation value must stay one argv element")
	assert.Greater(t, slices.Index(got, annotationValue), slices.Index(got, solo5ArgsSeparator),
		"the kernel path must come after %q", solo5ArgsSeparator)
}

func runSolo5GuestCommandOneElement(t *testing.T, vmm types.VMM) {
	t.Helper()

	command := `{"cmdline":"nginx -c /etc/nginx.conf"}`
	got, err := vmm.BuildExecCmd(types.ExecArgs{
		UnikernelPath: testKernelPath,
		MemSizeB:      256 * 1024 * 1024,
		Command:       command,
	}, &fakeUnikernel{})
	assert.NoError(t, err)

	if len(got) < 2 {
		t.Fatalf("expected at least the kernel path and its command, got %v", got)
	}
	assert.Equal(t, command, got[len(got)-1],
		"the guest command must be forwarded as a single argv element")
	assert.Equal(t, testKernelPath, got[len(got)-2],
		"the kernel path must directly precede the guest command")
}
