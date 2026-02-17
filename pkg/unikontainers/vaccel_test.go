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

package unikontainers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIdToGuestCID(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		expectedCID int
	}{
		{
			name:        "empty string",
			id:          "",
			expectedCID: 3,
		},
		{
			name:        "simple id",
			id:          "container123",
			expectedCID: 49,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := idToGuestCID(tt.id)

			assert.Equal(t, tt.expectedCID, got, "CID should match expected value")
		})
	}
}

func TestIsValidVSockAddress(t *testing.T) {
	tests := []struct {
		name                 string
		rpcAddress           string
		monitor              string
		expectedValid        bool
		expectedErr          bool
		expectedPath         string
		expectedModifiedAddr string
	}{
		{
			name:                 "valid qemu vsock address",
			rpcAddress:           "vsock://2:1234",
			monitor:              "qemu",
			expectedValid:        true,
			expectedErr:          false,
			expectedPath:         "",
			expectedModifiedAddr: "vsock://2:1234",
		},
		{
			name:          "invalid qemu vsock - wrong CID",
			rpcAddress:    "vsock://3:1234",
			monitor:       "qemu",
			expectedValid: false,
			expectedErr:   true,
		},
		{
			name:          "invalid qemu vsock - no port",
			rpcAddress:    "vsock://2:",
			monitor:       "qemu",
			expectedValid: false,
			expectedErr:   true,
		},
		{
			name:          "invalid qemu vsock - malformed",
			rpcAddress:    "vsock://invalid",
			monitor:       "qemu",
			expectedValid: false,
			expectedErr:   true,
		},
		{
			name:          "not a vsock address",
			rpcAddress:    "http://localhost:1234",
			monitor:       "qemu",
			expectedValid: false,
			expectedErr:   true,
		},
		{
			name:          "empty address",
			rpcAddress:    "",
			monitor:       "qemu",
			expectedValid: false,
			expectedErr:   true,
		},
		{
			name:                 "valid firecracker unix socket",
			rpcAddress:           "unix:///tmp/vaccel.sock_1234",
			monitor:              "firecracker",
			expectedValid:        true,
			expectedErr:          false,
			expectedPath:         "/tmp",
			expectedModifiedAddr: "vsock://2:1234",
		},
		{
			name:                 "valid firecracker nested path",
			rpcAddress:           "unix:///var/run/urunc/vaccel.sock_5678",
			monitor:              "firecracker",
			expectedValid:        true,
			expectedErr:          false,
			expectedPath:         "/var/run/urunc",
			expectedModifiedAddr: "vsock://2:5678",
		},
		{
			name:          "firecracker invalid - wrong socket name",
			rpcAddress:    "unix:///tmp/test.sock",
			monitor:       "firecracker",
			expectedValid: false,
			expectedErr:   true,
		},
		{
			name:          "firecracker invalid - no unix prefix",
			rpcAddress:    "/tmp/vaccel.sock_1234",
			monitor:       "firecracker",
			expectedValid: false,
			expectedErr:   true,
		},
		{
			name:          "unsupported monitor",
			rpcAddress:    "vsock://2:1234",
			monitor:       "kvm",
			expectedValid: false,
			expectedErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			addr := tt.rpcAddress
			gotValid, gotPath, err := isValidVSockAddress(&addr, tt.monitor)

			if tt.expectedErr {
				assert.Error(t, err, "isValidVSockAddress() should return an error")
			} else {
				assert.NoError(t, err, "isValidVSockAddress() should not return an error")
			}

			assert.Equal(t, tt.expectedValid, gotValid, "isValidVSockAddress() valid mismatch")

			if tt.expectedPath != "" {
				assert.Equal(t, tt.expectedPath, gotPath, "isValidVSockAddress() path mismatch")
			}

			// Check that rpcAddress is modified correctly (firecracker modifies it)
			if tt.expectedModifiedAddr != "" {
				assert.Equal(t, tt.expectedModifiedAddr, addr, "isValidVSockAddress() should modify rpcAddress correctly")
			}
		})
	}
}
