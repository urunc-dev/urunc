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
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestHyperlightBuildExecCmd(t *testing.T) {
	t.Parallel()

	h := &Hyperlight{
		binaryPath: "/usr/local/bin/hluk",
	}

	testCases := []struct {
		name      string
		args      types.ExecArgs
		unikernel types.Unikernel
		expected  []string
		wantErr   error
	}{
		{
			name: "kernel, initrd and memory",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel/kernel",
				InitrdPath:    "/unikernel/initrd.cpio",
				MemSizeB:      1024 * 1024 * 256,
			},
			unikernel: &fakeUnikernel{},
			expected:  []string{"/usr/local/bin/hluk", "run", "--kernel", "/unikernel/kernel", "--initrd", "/unikernel/initrd.cpio", "--scratch-mb", "256"},
		},
		{
			name: "no unikernel binary leaves the embedded kernel to hluk",
			args: types.ExecArgs{
				InitrdPath: "/unikernel/initrd.cpio",
			},
			unikernel: &fakeUnikernel{},
			expected:  []string{"/usr/local/bin/hluk", "run", "--initrd", "/unikernel/initrd.cpio"},
		},
		{
			name: "memory below one MiB is left to hluk",
			args: types.ExecArgs{
				InitrdPath: "/unikernel/initrd.cpio",
				MemSizeB:   1024,
			},
			unikernel: &fakeUnikernel{},
			expected:  []string{"/usr/local/bin/hluk", "run", "--initrd", "/unikernel/initrd.cpio"},
		},
		{
			name: "MonitorCli ExtraInitrd is the fallback initrd",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel/kernel",
			},
			unikernel: &fakeUnikernel{monitorCli: types.MonitorCliArgs{ExtraInitrd: "/extra/initrd.cpio"}},
			expected:  []string{"/usr/local/bin/hluk", "run", "--kernel", "/unikernel/kernel", "--initrd", "/extra/initrd.cpio"},
		},
		{
			name: "MonitorCli OtherArgs are appended verbatim",
			args: types.ExecArgs{
				InitrdPath: "/unikernel/initrd.cpio",
				MemSizeB:   1024 * 1024 * 512,
			},
			unikernel: &fakeUnikernel{monitorCli: types.MonitorCliArgs{OtherArgs: []string{"--guest-exec=/entrypoint.py --fast"}}},
			expected:  []string{"/usr/local/bin/hluk", "run", "--initrd", "/unikernel/initrd.cpio", "--scratch-mb", "512", "--guest-exec=/entrypoint.py --fast"},
		},
		{
			name: "snapshot resumes the guest instead of booting it",
			args: types.ExecArgs{
				SnapshotPath: "/unikernel/snapshot",
			},
			unikernel: &fakeUnikernel{},
			expected:  []string{"/usr/local/bin/hluk", "snapshot", "run", "/unikernel/snapshot"},
		},
		{
			name: "snapshot with MonitorCli OtherArgs appended verbatim",
			args: types.ExecArgs{
				SnapshotPath: "/unikernel/snapshot",
			},
			unikernel: &fakeUnikernel{monitorCli: types.MonitorCliArgs{OtherArgs: []string{"--guest-exec=/entrypoint.py", "--env=FOO=bar"}}},
			expected:  []string{"/usr/local/bin/hluk", "snapshot", "run", "/unikernel/snapshot", "--guest-exec=/entrypoint.py", "--env=FOO=bar"},
		},
		{
			name: "snapshot takes no kernel, initrd or memory",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel/kernel",
				InitrdPath:    "/unikernel/initrd.cpio",
				SnapshotPath:  "/unikernel/snapshot",
				MemSizeB:      1024 * 1024 * 256,
			},
			unikernel: &fakeUnikernel{monitorCli: types.MonitorCliArgs{ExtraInitrd: "/extra/initrd.cpio"}},
			expected:  []string{"/usr/local/bin/hluk", "snapshot", "run", "/unikernel/snapshot"},
		},
		{
			name: "no initrd is an error",
			args: types.ExecArgs{
				UnikernelPath: "/unikernel/kernel",
				MemSizeB:      1024 * 1024 * 256,
			},
			unikernel: &fakeUnikernel{},
			wantErr:   ErrHyperlightNoInitrd,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd, err := h.BuildExecCmd(tc.args, tc.unikernel)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, cmd)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, cmd)
		})
	}
}
