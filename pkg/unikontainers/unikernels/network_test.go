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

func TestGuestCommandWithoutDefaultGateway(t *testing.T) {
	t.Parallel()

	params := types.UnikernelParams{
		CmdLine: []string{"/init"},
		Net:     types.NetDevParams{IP: "172.18.5.2", Mask: "255.255.0.0"},
	}
	tests := []struct {
		name    string
		guest   types.Unikernel
		address string
		gateway string
	}{
		{"linux", &Linux{}, "ip=172.18.5.2:::255.255.0.0:urunc:eth0:off", "URUNIT_DEFROUTE="},
		{"mewz", &Mewz{}, "ip=172.18.5.2/16", "gateway="},
		{"mirage", &Mirage{}, "--ipv4=172.18.5.2/16", "--ipv4-gateway="},
		{"hermit", &Hermit{}, "ip=172.18.5.2/16", "ip=172.18.5.2/16:"},
		{"rumprun", &Rumprun{}, `"addr":"172.18.5.2","mask":"16"`, `"gw":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, tt.guest.Init(params))
			cmd, err := tt.guest.CommandString()
			require.NoError(t, err)
			assert.Contains(t, cmd, tt.address)
			assert.NotContains(t, cmd, tt.gateway)
		})
	}
}
