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

package main

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseSignal(t *testing.T) {
	tests := []struct {
		name       string
		rawSignal  string
		expected   unix.Signal
		shouldErr  bool
	}{
		{
			name:      "Valid named signal SIGKILL",
			rawSignal: "SIGKILL",
			expected:  unix.SIGKILL,
			shouldErr: false,
		},
		{
			name:      "Valid short named signal KILL",
			rawSignal: "KILL",
			expected:  unix.SIGKILL,
			shouldErr: false,
		},
		{
			name:      "Valid lowercase named signal kill",
			rawSignal: "kill",
			expected:  unix.SIGKILL,
			shouldErr: false,
		},
		{
			name:      "Valid named signal SIGTERM",
			rawSignal: "SIGTERM",
			expected:  unix.SIGTERM,
			shouldErr: false,
		},
		{
			name:      "Valid numeric signal 9",
			rawSignal: "9",
			expected:  unix.SIGKILL,
			shouldErr: false,
		},
		{
			name:      "Valid numeric signal 15",
			rawSignal: "15",
			expected:  unix.SIGTERM,
			shouldErr: false,
		},
		{
			name:      "Invalid numeric signal 0",
			rawSignal: "0",
			expected:  -1,
			shouldErr: true,
		},
		{
			name:      "Invalid negative numeric signal -1",
			rawSignal: "-1",
			expected:  -1,
			shouldErr: true,
		},
		{
			name:      "Invalid unknown signal string",
			rawSignal: "SIGUNKNOWN",
			expected:  -1,
			shouldErr: true,
		},
		{
			name:      "Invalid non-signal text",
			rawSignal: "FOOBAR",
			expected:  -1,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSignal(tt.rawSignal)
			if (err != nil) != tt.shouldErr {
				t.Fatalf("parseSignal(%q) error = %v, expected error = %v", tt.rawSignal, err, tt.shouldErr)
			}
			if !tt.shouldErr && got != tt.expected {
				t.Errorf("parseSignal(%q) = %v, expected %v", tt.rawSignal, got, tt.expected)
			}
		})
	}
}
