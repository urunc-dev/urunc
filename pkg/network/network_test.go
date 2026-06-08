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
