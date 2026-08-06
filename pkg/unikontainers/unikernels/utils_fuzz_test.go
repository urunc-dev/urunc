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
)

func FuzzSubnetMaskToCIDR(f *testing.F) {
	// Add seed corpus for valid and invalid masks
	f.Add("255.255.255.0")
	f.Add("255.0.0.0")
	f.Add("255.255.255.255")
	f.Add("0.0.0.0")
	f.Add("255.0.255.0") // the bug identified by ArkVex!
	f.Add("256.0.0.0")   // out of bounds

	f.Fuzz(func(t *testing.T, mask string) {
		// Ensure it doesn't panic
		cidr, err := subnetMaskToCIDR(mask)
		
		// If we wanted to assert property correctness:
		// A valid subnet mask must be contiguous 1s followed by 0s.
		// If err == nil, we could verify the string format exactly matches
		// the expected CIDR prefix, but for the fuzzing infra PR, 
		// avoiding panics and crashes is the baseline.
		_, _ = cidr, err
	})
}
