// Copyright (c) 2023-2025, Nubificus LTD
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

package metrics

import (
	"fmt"
	"testing"
)

func TestTimestampSchemaConsistency(t *testing.T) {
	seenLegacy := make(map[string]bool)
	for i, ts := range Timestamps {
		if ts.Order != i {
			t.Errorf("Order mismatch at index %d: got %d", i, ts.Order)
		}
		wantLegacy := fmt.Sprintf("TS%02d", i)
		if ts.LegacyID != wantLegacy {
			t.Errorf("LegacyID mismatch at index %d: got %s, want %s",
				i, ts.LegacyID, wantLegacy)
		}
		if seenLegacy[ts.LegacyID] {
			t.Errorf("duplicate LegacyID: %s", ts.LegacyID)
		}
		seenLegacy[ts.LegacyID] = true
	}
}
