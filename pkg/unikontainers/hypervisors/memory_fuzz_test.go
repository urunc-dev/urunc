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
	"strconv"
	"testing"
)

// FuzzBytesToStringMB: the returned string is embedded directly into a VMM
// memory argument read as binary MiB -- the guest must never get more
// memory than the OCI byte limit allowed.
func FuzzBytesToStringMB(f *testing.F) {
	// Seeds stay on the passing side of the boundary on purpose: the engine
	// has to reach the failing region by mutation rather than being handed a
	// reproducer.
	seeds := []uint64{
		0, 1, 999_999, 1_000_000, 1_048_576,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, argMem uint64) {
		if !strictOn("memory-limit") {
			return
		}
		got := BytesToStringMB(argMem)

		userMiB, err := strconv.ParseUint(got, 10, 64)
		if err != nil {
			t.Fatalf("BytesToStringMB(%d) returned non-numeric value %q: %v", argMem, got, err)
		}

		if argMem == 0 || argMem < 1_048_576 {
			// Below 1MiB, bytesToMiB truncates to 0 and this deliberately
			// falls back to DefaultMemory.
			return
		}

		effectiveBytes := userMiB * 1024 * 1024
		if effectiveBytes <= argMem {
			return
		}

		// The rounded MiB value must never exceed the requested bytes;
		// this fires if it does.
		t.Fatalf("BytesToStringMB(%d) = %q MiB -> %d bytes, which exceeds the requested OCI memory limit of %d bytes (overshoot: %d bytes)",
			argMem, got, effectiveBytes, argMem, effectiveBytes-argMem)
	})
}
