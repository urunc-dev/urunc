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

package hypervisors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBytesToMiB(t *testing.T) {
	t.Parallel()

	const mib uint64 = 1024 * 1024

	cases := []struct {
		name     string
		input    uint64
		expected uint64
	}{
		{"zero", 0, 0},
		{"less than one MiB truncates to zero", mib - 1, 0},
		{"exactly one MiB", mib, 1},
		{"exactly two MiB", 2 * mib, 2},
		{"non-multiple truncates down", mib + (mib / 2), 1},
		{"large value", 1024 * mib, 1024},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, bytesToMiB(tc.input))
		})
	}
}

func TestBytesToBString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    uint64
		expected string
	}{
		{"zero (Case A)", 0, "268435456"}, // 256 MB * 1024 * 1024
		{"exactly 512 bytes", 512, "512"},
		{"exactly 1024 bytes", 1024, "1024"},
		{"large value", 1024 * 1024 * 1024, "1073741824"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, BytesToBString(tc.input))
		})
	}
}

func TestBytesToMiBString(t *testing.T) {
	t.Parallel()

	const mib uint64 = 1024 * 1024

	cases := []struct {
		name     string
		input    uint64
		expected string
	}{
		{"zero (Case A)", 0, "256"},
		{"less than one MiB (Case B/C)", mib - 1, "0"},
		{"exactly one MiB", mib, "1"},
		{"exactly two MiB", 2 * mib, "2"},
		{"large value", 1024 * mib, "1024"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, BytesToMiBString(tc.input))
		})
	}
}
