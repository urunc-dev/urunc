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
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestUnikraftInitSubnetMask(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		mask        string
		ip          string
		gateway     string
		wantAddress string
		wantMask    string
		wantErr     error
	}{
		{
			name:        "non-/24 mask is used correctly",
			version:     "0.17.0",
			mask:        "255.255.255.240",
			ip:          "10.0.0.1",
			gateway:     "10.0.0.14",
			wantAddress: "netdev.ip=10.0.0.1/28:10.0.0.14:8.8.8.8",
		},
		{
			name:        "/16 mask is used correctly",
			version:     "0.17.0",
			mask:        "255.255.0.0",
			ip:          "10.244.1.3",
			gateway:     "10.244.0.1",
			wantAddress: "netdev.ip=10.244.1.3/16:10.244.0.1:8.8.8.8",
		},
		{
			name:        "/24 mask still works",
			version:     "0.17.0",
			mask:        "255.255.255.0",
			ip:          "192.168.1.5",
			gateway:     "192.168.1.1",
			wantAddress: "netdev.ip=192.168.1.5/24:192.168.1.1:8.8.8.8",
		},
		{
			name:        "empty mask falls back to /24",
			version:     "0.17.0",
			mask:        "",
			ip:          "192.168.1.5",
			gateway:     "192.168.1.1",
			wantAddress: "netdev.ip=192.168.1.5/24:192.168.1.1:8.8.8.8",
		},
		{
			name:        "undefined version uses actual mask with sentinel error",
			version:     "",
			mask:        "255.255.255.128",
			ip:          "10.0.0.2",
			gateway:     "10.0.0.1",
			wantAddress: "netdev.ip=10.0.0.2/25:10.0.0.1:8.8.8.8",
			wantErr:     ErrUndefinedVersion,
		},
		{
			name:     "old version passes mask through compat args",
			version:  "0.15.0",
			mask:     "255.255.255.240",
			ip:       "10.0.0.1",
			gateway:  "10.0.0.14",
			wantMask: "netdev.ipv4_subnet_mask=255.255.255.240",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := newUnikraft()
			params := types.UnikernelParams{
				Version: tt.version,
				Net: types.NetDevParams{
					IP:      tt.ip,
					Mask:    tt.mask,
					Gateway: tt.gateway,
				},
			}
			err := u.Init(params)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
			if tt.wantAddress != "" {
				assert.Equal(t, tt.wantAddress, u.Net.Address)
			}
			if tt.wantMask != "" {
				assert.Equal(t, tt.wantMask, u.Net.Mask)
			}
		})
	}
}

func TestUnikraftInitInvalidMask(t *testing.T) {
	u := newUnikraft()
	params := types.UnikernelParams{
		Version: "0.17.0",
		Net: types.NetDevParams{
			IP:      "10.0.0.1",
			Mask:    "255.255.255",
			Gateway: "10.0.0.14",
		},
	}
	err := u.Init(params)
	assert.Error(t, err)
}
