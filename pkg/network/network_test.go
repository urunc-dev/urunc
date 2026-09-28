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

package network

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewNetworkManager(t *testing.T) {
	tests := []struct {
		name        string
		networkType string
		expectedErr bool
	}{
		{
			name:        "static network manager",
			networkType: "static",
			expectedErr: false,
		},
		{
			name:        "dynamic network manager",
			networkType: "dynamic",
			expectedErr: false,
		},
		{
			name:        "invalid network type",
			networkType: "invalid",
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewNetworkManager(tt.networkType)
			if tt.expectedErr {
				assert.Error(t, err, "NewNetworkManager() should return an error")
			} else {
				assert.NoError(t, err, "NewNetworkManager() should not return an error")
				assert.NotNil(t, got, "NewNetworkManager() should return a non-nil manager")
			}
		})
	}
}

func TestGetTapIndex(t *testing.T) {
	tapCount, err := getTapIndex()
	assert.NoError(t, err, "getTapIndex() should not error")
	assert.GreaterOrEqual(t, tapCount, 0, "tapCount should be >= 0")
}

func TestTapDeviceRegexMatching(t *testing.T) {
	tapRe := regexp.MustCompile(`^tap\d+(_urunc)?$`)

	tests := []struct {
		ifaceName   string
		shouldMatch bool
	}{
		{"tap0_urunc", true},
		{"tap1_urunc", true},
		{"tap0", true},
		{"tap12", true},
		{"vtap0", false},
		{"cni-tap0", false},
		{"bootstrap0", false},
		{"stape0", false},
		{"tap-master", false},
		{"eth0", false},
		{"lo", false},
	}

	for _, tt := range tests {
		t.Run(tt.ifaceName, func(t *testing.T) {
			matched := tapRe.MatchString(tt.ifaceName)
			assert.Equal(t, tt.shouldMatch, matched, "interface name %s match expectation failed", tt.ifaceName)
		})
	}
}
