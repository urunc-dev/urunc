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
)

func TestSubnetMaskToCIDR(t *testing.T) {
	tests := []struct {
		name      string
		mask      string
		wantCIDR  int
		expectErr bool
	}{
		// Canonical masks (/0 - /32)
		{name: "mask /0", mask: "0.0.0.0", wantCIDR: 0, expectErr: false},
		{name: "mask /1", mask: "128.0.0.0", wantCIDR: 1, expectErr: false},
		{name: "mask /8", mask: "255.0.0.0", wantCIDR: 8, expectErr: false},
		{name: "mask /16", mask: "255.255.0.0", wantCIDR: 16, expectErr: false},
		{name: "mask /24", mask: "255.255.255.0", wantCIDR: 24, expectErr: false},
		{name: "mask /28", mask: "255.255.255.240", wantCIDR: 28, expectErr: false},
		{name: "mask /30", mask: "255.255.255.252", wantCIDR: 30, expectErr: false},
		{name: "mask /32", mask: "255.255.255.255", wantCIDR: 32, expectErr: false},

		// Non-contiguous netmasks (issue #909)
		{name: "non-contiguous 255.0.255.0", mask: "255.0.255.0", wantCIDR: 0, expectErr: true},
		{name: "non-contiguous 0.255.255.255", mask: "0.255.255.255", wantCIDR: 0, expectErr: true},
		{name: "non-contiguous 255.255.1.0", mask: "255.255.1.0", wantCIDR: 0, expectErr: true},
		{name: "non-contiguous 255.255.255.2", mask: "255.255.255.2", wantCIDR: 0, expectErr: true},
		{name: "non-contiguous 255.255.255.253", mask: "255.255.255.253", wantCIDR: 0, expectErr: true},

		// Invalid string formats
		{name: "invalid string", mask: "invalid", wantCIDR: 0, expectErr: true},
		{name: "missing octet", mask: "255.255.255", wantCIDR: 0, expectErr: true},
		{name: "out of range octet", mask: "255.255.255.256", wantCIDR: 0, expectErr: true},
		{name: "too many octets", mask: "255.255.255.0.0", wantCIDR: 0, expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cidr, err := subnetMaskToCIDR(tt.mask)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantCIDR, cidr)
			}
		})
	}
}
