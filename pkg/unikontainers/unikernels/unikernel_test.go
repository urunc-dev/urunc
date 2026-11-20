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
	"github.com/stretchr/testify/require"
)

func TestNewUnikernel(t *testing.T) {
	t.Parallel()

	t.Run("supported unikernel types", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name          string
			unikernelType string
			expectedType  any
		}{
			{name: "rumprun", unikernelType: RumprunUnikernel, expectedType: &Rumprun{}},
			{name: "unikraft", unikernelType: UnikraftUnikernel, expectedType: &Unikraft{}},
			{name: "mirage", unikernelType: MirageUnikernel, expectedType: &Mirage{}},
			{name: "mewz", unikernelType: MewzUnikernel, expectedType: &Mewz{}},
			{name: "linux", unikernelType: LinuxUnikernel, expectedType: &Linux{}},
			{name: "hermit", unikernelType: HermitUnikernel, expectedType: &Hermit{}},
			{name: "freebsd", unikernelType: FreeBSDUnikernel, expectedType: &FreeBSD{}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				u, err := New(tc.unikernelType)
				require.NoError(t, err)
				require.NotNil(t, u)
				assert.IsType(t, tc.expectedType, u)
			})
		}
	})

	t.Run("unsupported unikernel types", func(t *testing.T) {
		t.Parallel()

		invalidTypes := []string{"", "unknown", "LINUX", "unikernel"}
		for _, invalidType := range invalidTypes {
			t.Run("invalid_"+invalidType, func(t *testing.T) {
				t.Parallel()

				u, err := New(invalidType)
				assert.Nil(t, u)
				assert.ErrorIs(t, err, ErrNotSupportedUnikernel)
			})
		}
	})
}
