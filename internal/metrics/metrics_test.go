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
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
)

func TestZerologMetricsMetadata(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	writer := &zerologMetrics{
		logger:      &logger,
		containerID: "container00",
	}

	writer.Capture(TS00)

	line := buf.String()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("failed to parse log: %v", err)
	}

	if got := m["containerID"]; got != "container00" {
		t.Errorf("containerID = %v, want container00", got)
	}
	if got := m["timestampID"]; got != "TS00" {
		t.Errorf("timestampID = %v, want TS00", got)
	}
	if got := m["timestampName"]; got != "CR.invoked" {
		t.Errorf("timestampName = %v, want CR.invoked", got)
	}
	if got := int(m["timestampOrder"].(float64)); got != 0 {
		t.Errorf("timestampOrder = %v, want 0", got)
	}
}
