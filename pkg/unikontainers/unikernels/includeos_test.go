// Copyright (c) 2023-2025, Nubificus LTD
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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
)

func TestIncludeOS_CommandString(t *testing.T) {
	tests := []struct {
		name      string
		unikernel *IncludeOS
		expected  []string
	}{
		{
			name: "Full Configuration",
			unikernel: &IncludeOS{
				Command: "my_app_arg",
				Envs:    []string{"FOO=bar", "BAZ=qux"},
				Net: IncludeOSNet{
					Address: "192.168.1.5",
					Mask:    "255.255.255.0",
					Gateway: "192.168.1.1",
				},
			},
			expected: []string{
				`{"net":[{"iface":0,"address":"192.168.1.5","netmask":"255.255.255.0","gateway":"192.168.1.1"}]}`,
				"FOO=bar",
				"BAZ=qux",
				"my_app_arg",
			},
		},
		{
			name: "Network Only (No Gateway)",
			unikernel: &IncludeOS{
				Net: IncludeOSNet{
					Address: "10.0.0.2",
					Mask:    "255.255.0.0",
				},
			},
			expected: []string{
				`{"net":[{"iface":0,"address":"10.0.0.2","netmask":"255.255.0.0"}]}`,
			},
		},
		{
			name: "No Network (Command Only)",
			unikernel: &IncludeOS{
				Command: "just_running",
			},
			expected: []string{
				"just_running",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.unikernel.CommandString()
			if err != nil {
				t.Fatalf("CommandString() error = %v", err)
			}

			for _, part := range tt.expected {
				if !strings.Contains(got, part) {
					t.Errorf("CommandString() = %v\nMissing expected part: %v", got, part)
				}
			}

			if tt.name == "No Network (Command Only)" {
				if strings.Contains(got, "net") {
					t.Errorf("CommandString() should not contain network config, got: %v", got)
				}
			}
		})
	}
}
