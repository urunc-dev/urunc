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

	testCases := []struct {
		name     string
		hermit   *Hermit
		expected string
	}{
		{
			name:     "no network configured",
			hermit:   &Hermit{},
			expected: "",
		},
		{
			name: "address and gateway",
			hermit: &Hermit{
				Net: HermitNet{
					Address: "10.0.0.2",
					Mask:    24,
					Gateway: "10.0.0.1",
				},
			},
			expected: "ip=10.0.0.2/24:10.0.0.1",
		},
		{
			name: "address without gateway",
			hermit: &Hermit{
				Net: HermitNet{
					Address: "10.0.0.2",
					Mask:    24,
				},
			},
			expected: "ip=10.0.0.2/24",
		},
		{
			name: "with DNS configured",
			hermit: &Hermit{
				Net: HermitNet{
					Address:   "10.0.0.2",
					Mask:      24,
					Gateway:   "10.0.0.1",
					DNSServer: "1.1.1.1",
				},
			},
			expected: "ip=10.0.0.2/24:10.0.0.1 env=HERMIT_DNS1=1.1.1.1",
		},
		{
			name: "without DNS configured",
			hermit: &Hermit{
				Net: HermitNet{
					Address: "10.0.0.2",
					Mask:    24,
					Gateway: "10.0.0.1",
				},
			},
			expected: "ip=10.0.0.2/24:10.0.0.1",
		},
		{
			name: "network and application arguments",
			hermit: &Hermit{
				Command: "/app --port 8080",
				Net: HermitNet{
					Address: "10.0.0.2",
					Mask:    24,
					Gateway: "10.0.0.1",
				},
			},
			expected: "ip=10.0.0.2/24:10.0.0.1 -- /app --port 8080",
		},
		{
			name: "application arguments without network",
			hermit: &Hermit{
				Command: "/app --port 8080",
			},
			expected: "/app --port 8080",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := tc.hermit.CommandString()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestHermitInitDNSServer(t *testing.T) {
	t.Parallel()

	h := &Hermit{}

	data := types.UnikernelParams{
		Net: types.NetDevParams{
			IP:        "10.0.0.2",
			Mask:      "255.255.255.0",
			Gateway:   "10.0.0.1",
			DNSServer: "1.1.1.1",
		},
	}

	err := h.Init(data)

	require.NoError(t, err)
	assert.Equal(t, "1.1.1.1", h.Net.DNSServer)
}
