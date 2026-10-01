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

package unikernels

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestUnikraftCommandString(t *testing.T) {
	t.Parallel()

	consoleStr := ""
	if runtime.GOARCH == "arm64" {
		consoleStr = "console=ttyS0"
	}

	testCases := []struct {
		name     string
		unikraft *Unikraft
		expected string
	}{
		{
			name: "basic command with no network or env",
			unikraft: &Unikraft{
				AppName: "Unikraft",
				Command: "rootfs=/dev/root",
			},
			expected: strings.TrimSpace("Unikraft " + consoleStr + "      -- rootfs=/dev/root"),
		},
		{
			name: "with environment variables",
			unikraft: &Unikraft{
				AppName: "Unikraft",
				Command: "",
				Env:     []string{"ENV1=val1", "ENV2=val2"},
			},
			expected: strings.TrimSpace("Unikraft " + consoleStr + " env.vars=[ ENV1=val1 ENV2=val2 ]     -- "),
		},
		{
			name: "with network parameters",
			unikraft: &Unikraft{
				AppName: "Unikraft",
				Command: "",
				Net: UnikraftNet{
					Address: "netdev.ip=10.0.0.2/24:10.0.0.1:8.8.8.8",
				},
			},
			// The current CommandString logic formats strings separated by spaces.
			// When Gateway and Mask and VFS are empty strings, there will be empty spaces.
			expected: strings.TrimSpace("Unikraft " + consoleStr + "  netdev.ip=10.0.0.2/24:10.0.0.1:8.8.8.8    -- "),
		},
		{
			name: "with VFS rootfs (initrd)",
			unikraft: &Unikraft{
				AppName: "Unikraft",
				Command: "app.run",
				VFS: UnikraftVFS{
					RootFS: "vfs.fstab=[ \"initrd0:/:extract:::\" ]",
				},
			},
			expected: strings.TrimSpace("Unikraft " + consoleStr + "      vfs.fstab=[ \"initrd0:/:extract:::\" ] -- app.run"),
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := tc.unikraft.CommandString()
			assert.NoError(t, err)

			// Clean up extra spaces caused by empty struct fields in the sprintf
			cleanResult := strings.Join(strings.Fields(result), " ")
			cleanExpected := strings.Join(strings.Fields(tc.expected), " ")

			assert.Equal(t, cleanExpected, cleanResult)
		})
	}
}

func TestUnikraftSupportsFS(t *testing.T) {
	t.Parallel()

	uk := newUnikraft()

	assert.True(t, uk.SupportsFS("9pfs"))
	assert.False(t, uk.SupportsFS("ext2"))
	assert.False(t, uk.SupportsFS("virtiofs"))
}

func TestUnikraftSupportsBlock(t *testing.T) {
	t.Parallel()

	uk := newUnikraft()
	assert.False(t, uk.SupportsBlock())
}

func TestUnikraftInitVersionHandling(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		version       string
		rootFsType    string
		expectedErr   error
		expectedEnv   []string
		expectedAddr  string
		expectedVFS   string
	}{
		{
			name:          "version undefined",
			version:       "",
			rootFsType:    "initrd",
			expectedErr:   ErrUndefinedVersion,
			expectedEnv:   []string{"TEST=true"},
			expectedAddr:  "netdev.ip=10.0.0.2/24:10.0.0.1:8.8.8.8",
			expectedVFS:   "vfs.fstab=[ \"initrd0:/:extract:::\" ]",
		},
		{
			name:          "version parsing failure",
			version:       "invalid-version",
			rootFsType:    "initrd",
			expectedErr:   ErrVersionParsing,
			expectedEnv:   []string{"TEST=true"},
			expectedAddr:  "netdev.ip=10.0.0.2/24:10.0.0.1:8.8.8.8",
			expectedVFS:   "vfs.fstab=[ \"initrd0:/:extract:::\" ]",
		},
		{
			name:          "current version >= 0.16.1",
			version:       "0.16.1",
			rootFsType:    "9pfs",
			expectedErr:   nil,
			expectedEnv:   []string{"TEST=true"},
			expectedAddr:  "netdev.ip=10.0.0.2/24:10.0.0.1:8.8.8.8",
			expectedVFS:   "vfs.fstab=[ \"fs0:/:9pfs:::\" ]",
		},
		{
			name:          "legacy version < 0.16.1",
			version:       "0.15.0",
			rootFsType:    "initrd",
			expectedErr:   nil,
			expectedEnv:   nil, // Legacy versions strip environment variables
			expectedAddr:  "netdev.ipv4_addr=10.0.0.2",
			expectedVFS:   "vfs.rootfs=initrd",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			uk := newUnikraft()
			params := types.UnikernelParams{
				Version: tc.version,
				EnvVars: []string{"TEST=true"},
				Rootfs: types.RootfsParams{
					Type: tc.rootFsType,
				},
				Net: types.NetDevParams{
					IP:      "10.0.0.2",
					Gateway: "10.0.0.1",
					Mask:    "255.255.255.0",
				},
			}

			err := uk.Init(params)

			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tc.expectedEnv, uk.Env)
			assert.Equal(t, tc.expectedAddr, uk.Net.Address)
			assert.Equal(t, tc.expectedVFS, uk.VFS.RootFS)
		})
	}
}
