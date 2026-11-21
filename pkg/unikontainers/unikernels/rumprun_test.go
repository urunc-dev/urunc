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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// The guest splits a "cmdline" value on whitespace (config.c's makeargv and
// rumprun_parseargs), so arguments that contain spaces and empty arguments
// cannot be expressed in that form. The "rc" form keeps every argv element
// intact: config.c's addbin copies each array element as its own string.
func TestRumprunCommandStringPreservesArgumentBoundaries(t *testing.T) {
	t.Parallel()

	rumprun := &Rumprun{
		CmdLine: []string{"redis-server", "--requirepass", "a b", "--logfile", ""},
		Envs:    []string{"REDIS_CONF=/etc/redis.conf"},
	}

	commandString, err := rumprun.CommandString()
	require.NoError(t, err)

	// The result is not valid JSON as a whole (the multiple "env" keys), so
	// check the rc member against its exact serialization.
	assert.Contains(t, commandString,
		`"rc":[{"bin":"redis-server","argv":["--requirepass","a b","--logfile",""]}]`,
		"every argument must stay one element, including the spaced value and the empty one")

	var parsed struct {
		Rc []struct {
			Bin  string   `json:"bin"`
			Argv []string `json:"argv"`
		} `json:"rc"`
	}
	require.NoError(t, json.Unmarshal([]byte(commandString), &parsed))
	require.Len(t, parsed.Rc, 1)
	assert.Equal(t, "redis-server", parsed.Rc[0].Bin)
	assert.Equal(t, []string{"--requirepass", "a b", "--logfile", ""}, parsed.Rc[0].Argv)
}

// The guest's token2cstr keeps string bytes as they appear, without decoding
// \uXXXX escapes, so the configuration must not escape &, < or >.
func TestRumprunCommandStringKeepsHTMLEScapeCharactersRaw(t *testing.T) {
	t.Parallel()

	rumprun := &Rumprun{
		CmdLine: []string{"sh", "-c", "echo a<b&c>d"},
	}

	commandString, err := rumprun.CommandString()
	require.NoError(t, err)

	assert.Contains(t, commandString, `"argv":["-c","echo a<b&c>d"]`)
	assert.NotContains(t, commandString, `u003`, "the guest cannot decode \\uXXXX escapes")
}

// An empty command line has no rc entry to describe; it keeps the old empty
// cmdline, which lets the guest run its first baked program without arguments.
func TestRumprunCommandStringEmptyCmdLineKeepsEmptyCmdline(t *testing.T) {
	t.Parallel()

	rumprun := &Rumprun{CmdLine: nil}

	commandString, err := rumprun.CommandString()
	require.NoError(t, err)

	assert.Contains(t, commandString, `"cmdline":""`)
	assert.NotContains(t, commandString, `"rc"`)
}

// Init must hand the whole argument list to CommandString instead of
// collapsing it into one string first.
func TestRumprunInitKeepsCmdLineUnjoined(t *testing.T) {
	t.Parallel()

	rumprun := newRumprun()
	err := rumprun.Init(types.UnikernelParams{
		CmdLine: []string{"app", "one arg", "another"},
		Monitor: "hvt",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"app", "one arg", "another"}, rumprun.CmdLine)
}
