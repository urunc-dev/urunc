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

func TestIsIPInSubnet(t *testing.T) {
	tests := []struct {
		name string
		net  LinuxNet
		want bool
	}{
		{
			name: "same subnet",
			net:  LinuxNet{Address: "10.0.0.5", Gateway: "10.0.0.1", Mask: "255.255.255.0"},
			want: true,
		},
		{
			name: "different subnet",
			net:  LinuxNet{Address: "10.244.0.5", Gateway: "169.254.1.1", Mask: "255.255.255.0"},
			want: false,
		},
		{
			name: "empty mask",
			net:  LinuxNet{Address: "10.244.0.5", Gateway: "169.254.1.1", Mask: ""},
			want: false,
		},
		{
			name: "garbage mask",
			net:  LinuxNet{Address: "10.244.0.5", Gateway: "169.254.1.1", Mask: "abc"},
			want: false,
		},
		{
			name: "truncated mask",
			net:  LinuxNet{Address: "10.244.0.5", Gateway: "169.254.1.1", Mask: "255.255.255"},
			want: false,
		},
		{
			name: "IPv6 mask",
			net:  LinuxNet{Address: "10.244.0.5", Gateway: "169.254.1.1", Mask: "ffff:ffff::"},
			want: false,
		},
		{
			name: "CIDR-style mask",
			net:  LinuxNet{Address: "10.244.0.5", Gateway: "169.254.1.1", Mask: "/24"},
			want: false,
		},
		{
			name: "all empty",
			net:  LinuxNet{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsIPInSubnet(tt.net))
		})
	}
}
