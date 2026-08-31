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

func TestChooseTmpfsSize(t *testing.T) {
	const (
		mem256MB uint64 = 256 * 1024 * 1024
		mem512MB uint64 = 512 * 1024 * 1024
	)
	tests := []struct {
		name     string
		sfsType  string
		mem      uint64
		expected string
	}{
		{
			name:     "9pfs returns constant regardless of memory",
			sfsType:  "9pfs",
			mem:      mem256MB,
			expected: tmpfsSizeFor9pfsRootfs,
		},
		{
			name:     "virtiofs 256 MB adds 1 MiB overhead",
			sfsType:  "virtiofs",
			mem:      mem256MB,
			expected: "269m",
		},
		{
			name:     "virtiofs 512 MB adds 1 MiB overhead",
			sfsType:  "virtiofs",
			mem:      mem512MB,
			expected: "537m",
		},
		{
			name:     "virtiofs zero memory uses 1 MiB overhead only",
			sfsType:  "virtiofs",
			mem:      0,
			expected: "1m",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := chooseTmpfsSize(tt.sfsType, tt.mem)
			assert.Equal(t, tt.expected, got)
		})
	}
}
