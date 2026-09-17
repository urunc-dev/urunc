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
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdToGuestCID(t *testing.T) {
	t.Parallel()

	ids := []string{
		"",
		"container123",
		// containerd IDs: 64 hex digits, often differing in a few characters.
		"359ba00f1ea2be560fd5e291cc819ef7f523a09501f9c398f20fdc1c27478cf6",
		"359ba00f1ea2be560fd5e291cc819ef7f523a09501f9c398f20fdc1c27478cf7",
		"459ba00f1ea2be560fd5e291cc819ef7f523a09501f9c398f20fdc1c27478cf6",
		"b5dd62f96cc6c64e16388d439540cd18d4b21756b6d0e3f0ca1e9a6968dbeba6",
	}

	seen := map[int]string{}
	for _, id := range ids {
		got := idToGuestCID(id)

		// Every CID is valid for a guest: not 0, 1 (reserved), 2 (the host)
		// or 2^32-1 (any), and it fits in 32 bits.
		assert.GreaterOrEqual(t, got, minGuestCID, "CID of %q below the guest range", id)
		assert.LessOrEqual(t, got, maxGuestCID, "CID of %q above the guest range", id)

		// Deterministic: the monitor, urunc exec and vAccel must all agree.
		assert.Equal(t, got, idToGuestCID(id), "CID of %q is not stable", id)

		// Distinct for these IDs, including ones differing in one character,
		// which the old character-sum hash mapped to nearby or equal values.
		if prev, dup := seen[got]; dup {
			t.Errorf("IDs %q and %q share CID %d", prev, id, got)
		}
		seen[got] = id
	}

	// The whole 32-bit range is used, not a small window of it.
	spread := false
	for _, id := range ids {
		if idToGuestCID(id) > 1<<16 {
			spread = true
		}
	}
	assert.True(t, spread, "CIDs should span the full 32-bit range")
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
			expectedPath:         "/tmp/vaccel.sock_1234",
			expectedModifiedAddr: "vsock://2:1234",
		},
		{
			name:                 "valid firecracker nested path",
			rpcAddress:           "unix:///var/run/urunc/vaccel.sock_5678",
			monitor:              "firecracker",
			expectedValid:        true,
			expectedErr:          false,
			expectedPath:         "/var/run/urunc/vaccel.sock_5678",
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

func TestResolveVAccelConfig(t *testing.T) {
	t.Run("vaccel disabled returns ErrVAccelDisabled", func(t *testing.T) {
		t.Parallel()
		annotations := map[string]string{}
		_, _, _, err := resolveVAccelConfig("qemu", annotations)
		assert.ErrorIs(t, err, ErrVAccelDisabled)
	})

	t.Run("vaccel enabled but rpc address missing returns error", func(t *testing.T) {
		t.Parallel()
		annotations := map[string]string{
			"com.urunc.unikernel.vAccel": "vsock",
		}
		_, _, _, err := resolveVAccelConfig("qemu", annotations)
		assert.Error(t, err)
		assert.False(t, errors.Is(err, ErrVAccelDisabled), "missing rpc address should not be ErrVAccelDisabled")
	})

	t.Run("vaccel enabled with malformed rpc address returns error", func(t *testing.T) {
		t.Parallel()
		annotations := map[string]string{
			"com.urunc.unikernel.vAccel":     "vsock",
			"com.urunc.unikernel.RPCAddress": "invalid-address",
		}
		_, _, _, err := resolveVAccelConfig("qemu", annotations)
		assert.Error(t, err)
		assert.False(t, errors.Is(err, ErrVAccelDisabled), "malformed address should not be ErrVAccelDisabled")
	})

	t.Run("vaccel enabled with valid qemu vsock address succeeds", func(t *testing.T) {
		t.Parallel()
		annotations := map[string]string{
			"com.urunc.unikernel.vAccel":     "vsock",
			"com.urunc.unikernel.RPCAddress": "vsock://2:1234",
		}
		vAccelType, _, addr, err := resolveVAccelConfig("qemu", annotations)
		assert.NoError(t, err)
		assert.Equal(t, "vsock", vAccelType)
		assert.Equal(t, "vsock://2:1234", addr)
	})
}

// listenVAccelSocket starts a listener on a unix socket named like the vAccel
// agent's one, in a fresh directory whose path matches vAccelSockDirRe (which
// t.TempDir can not guarantee), and returns the path of the socket.
func listenVAccelSocket(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "vaccel")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	path := filepath.Join(dir, "vaccel.sock_2049")
	listener, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })

	return path
}

func TestCheckVAccelSocket(t *testing.T) {
	t.Run("accepts a listening unix socket", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, checkVAccelSocket(listenVAccelSocket(t)))
	})

	t.Run("rejects a regular file", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "vaccel.sock_2049")
		require.NoError(t, os.WriteFile(path, []byte("not a socket"), 0o600))

		assert.Error(t, checkVAccelSocket(path))
	})

	t.Run("rejects a directory", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, checkVAccelSocket(t.TempDir()))
	})

	t.Run("rejects a missing path", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, checkVAccelSocket(filepath.Join(t.TempDir(), "vaccel.sock_2049")))
	})
}
