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
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLinuxUrunitArgs covers the arguments that the kernel command line
// mangles: an empty argument and a literal "--".
func TestLinuxUrunitArgs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		cmdLine  []string
		expected string
	}{
		{
			name:     "empty argument",
			cmdLine:  []string{"/urunit", "/app", "a", "", "b"},
			expected: "ARC:4\nARV:/app\nARV:a\nARV:\nARV:b\nUCE\n",
		},
		{
			name:     "literal --",
			cmdLine:  []string{"/urunit", "/app", "a", "--", "b", "c"},
			expected: "ARC:5\nARV:/app\nARV:a\nARV:--\nARV:b\nARV:c\nUCE\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := &Linux{InitrdConf: true, RootFsType: "virtiofs"}
			require.NoError(t, l.parseCmdLine(tc.cmdLine))
			assert.Contains(t, l.buildUrunitConfig(), tc.expected)

			cmd, err := l.CommandString()
			require.NoError(t, err)
			assert.True(t, strings.HasSuffix(cmd, " init=/urunit"), "unexpected command line: %s", cmd)
		})
	}
}

// TestLinuxUrunitLongArgs covers an argument list longer than the kernel
// command line (2048 bytes) with more than 32 arguments.
func TestLinuxUrunitLongArgs(t *testing.T) {
	t.Parallel()

	cmdLine := []string{"/urunit", "/app"}
	for i := range 40 {
		cmdLine = append(cmdLine, strconv.Itoa(i)+strings.Repeat("x", 100))
	}
	l := &Linux{InitrdConf: true, RootFsType: "virtiofs"}
	require.NoError(t, l.parseCmdLine(cmdLine))
	conf := l.buildUrunitConfig()

	assert.Contains(t, conf, "\nARC:41\n")
	assert.Equal(t, 41, strings.Count(conf, "\nARV:"))
	assert.Contains(t, conf, "\nARV:"+cmdLine[41]+"\nUCE\n")

	cmd, err := l.CommandString()
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(cmd, " init=/urunit"), "unexpected command line: %s", cmd)
}
