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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestQuoteUnikraftArg(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		arg      string
		expected string
	}{
		{name: "a plain argument is kept as is", arg: "/usr/bin/nginx", expected: "/usr/bin/nginx"},
		{name: "an option with a value is kept as is", arg: "--level=info", expected: "--level=info"},
		{name: "a space is kept in single quotes", arg: "hello world", expected: "'hello world'"},
		{name: "a tab is kept in single quotes", arg: "a\tb", expected: "'a\tb'"},
		{name: "double quotes are kept in single quotes", arg: `URUNC={"level":"info"}`, expected: `'URUNC={"level":"info"}'`},
		{name: "a single quote is written as '\\''", arg: "URUNC=don't", expected: `'URUNC=don'\''t'`},
		{name: "a backslash is kept in single quotes", arg: `a\b`, expected: `'a\b'`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, quoteUnikraftArg(tc.arg))
		})
	}
}

func TestUnikraftCommandStringQuotesEnvAndArgs(t *testing.T) {
	t.Parallel()

	u := newUnikraft()
	err := u.Init(types.UnikernelParams{
		CmdLine: []string{"/usr/bin/nginx", "-g", "daemon off;"},
		EnvVars: []string{"URUNC=hello world", "PATH=/usr/bin:/bin"},
		Monitor: "qemu",
		Version: "0.18.0",
		Net:     types.NetDevParams{IP: "10.4.0.15", Gateway: "10.4.0.1", Mask: "255.255.255.0"},
		Rootfs:  types.RootfsParams{Type: "initrd"},
	})
	require.NoError(t, err)

	cmd, err := u.CommandString()
	require.NoError(t, err)
	assert.Contains(t, cmd, "env.vars=[ 'URUNC=hello world' PATH=/usr/bin:/bin ]")
	assert.True(t, strings.HasSuffix(cmd, " -- /usr/bin/nginx -g 'daemon off;'"), cmd)
}

func TestUnikraftHyperlightCommandIsNotQuoted(t *testing.T) {
	t.Parallel()

	u := newUnikraft()
	err := u.Init(types.UnikernelParams{
		CmdLine: []string{"/entrypoint.py", "hello world"},
		Monitor: hyperlightMonitor,
		Version: "0.18.0",
	})
	require.NoError(t, err)

	assert.Equal(t, types.MonitorCliArgs{OtherArgs: []string{"--guest-exec=/entrypoint.py hello world"}}, u.MonitorCli())
}
