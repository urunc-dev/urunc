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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubnetMaskToCIDR(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		mask        string
		expected    int
		expectError bool
	}{
		{name: "class C /24", mask: "255.255.255.0", expected: 24},
		{name: "class B /16", mask: "255.255.0.0", expected: 16},
		{name: "class A /8", mask: "255.0.0.0", expected: 8},
		{name: "host mask /32", mask: "255.255.255.255", expected: 32},
		{name: "zero mask /0", mask: "0.0.0.0", expected: 0},
		{name: "non-octet boundary /20", mask: "255.255.240.0", expected: 20},
		{name: "empty string", mask: "", expectError: true},
		{name: "too few octets", mask: "255.255.255", expectError: true},
		{name: "too many octets", mask: "255.255.255.0.0", expectError: true},
		{name: "non-numeric octet", mask: "255.abc.255.0", expectError: true},
		{name: "negative octet", mask: "255.-1.0.0", expectError: true},
		{name: "octet above 255", mask: "255.256.0.0", expectError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cidr, err := subnetMaskToCIDR(tc.mask)
			if tc.expectError {
				assert.Error(t, err)
				assert.Zero(t, cidr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expected, cidr)
		})
	}
}

func TestCreateFile(t *testing.T) {
	t.Parallel()

	t.Run("creates file with expected content", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.txt")
		content := "KEY=VALUE\nSECOND=123\n"

		err := createFile(path, content)
		require.NoError(t, err)

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})

	t.Run("overwrites existing file content", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "existing.txt")
		require.NoError(t, os.WriteFile(path, []byte("old content"), 0o600))

		err := createFile(path, "new content")
		require.NoError(t, err)

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "new content", string(data))
	})

	t.Run("fails when parent directory does not exist", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "missing-dir", "config.txt")
		err := createFile(path, "hello")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create file")
	})
}
