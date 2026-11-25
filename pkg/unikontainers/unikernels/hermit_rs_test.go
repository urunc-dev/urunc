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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestHermitCommandString(t *testing.T) {
	t.Parallel()

	network := types.NetDevParams{
		IP:      "10.0.0.2",
		Mask:    "255.255.255.0",
		Gateway: "10.0.0.1",
	}
	testCases := []struct {
		name     string
		net      types.NetDevParams
		cmdLine  []string
		expected string
	}{
		{name: "no network or arguments"},
		{
			name:     "address and gateway",
			net:      network,
			expected: "ip=10.0.0.2/24:10.0.0.1",
		},
		{
			name:     "address without gateway",
			net:      types.NetDevParams{IP: network.IP, Mask: network.Mask},
			expected: "ip=10.0.0.2/24",
		},
		{
			name:     "network and application arguments",
			net:      network,
			cmdLine:  []string{"/app", "-g", "daemon off;"},
			expected: "ip=10.0.0.2/24:10.0.0.1 -- '/app' '-g' 'daemon off;'",
		},
		{
			name:     "application flags without network",
			cmdLine:  []string{"--port=80", "x", "--"},
			expected: "-- '--port=80' 'x' '--'",
		},
		{
			name:     "one empty argument",
			cmdLine:  []string{""},
			expected: "-- ''",
		},
		{
			name:     "comments and whitespace",
			cmdLine:  []string{"a", "#b", "", " ", "c\td\ne", ""},
			expected: "-- 'a' '#b' '' ' ' 'c\td\ne' ''",
		},
		{
			name:     "quotes and backslashes",
			cmdLine:  []string{`O'Brien`, `"`, `{"k":"v"}`, `C:\dir\`},
			expected: `-- 'O'\''Brien' '"' '{"k":"v"}' 'C:\dir\'`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHermit()
			require.NoError(t, h.Init(types.UnikernelParams{CmdLine: tc.cmdLine, Net: tc.net}))
			result, err := h.CommandString()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}
