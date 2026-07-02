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

package metrics

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	err := json.Unmarshal([]byte(line), &m)
	require.NoError(t, err, "failed to parse log output")

	assert.Equal(t, "container00", m["containerID"], "containerID mismatch")
	assert.Equal(t, "TS00", m["timestampID"], "timestampID mismatch")
	assert.Equal(t, "CR.invoked", m["timestampName"], "timestampName mismatch")
	assert.Equal(t, float64(0), m["timestampOrder"], "timestampOrder mismatch")
}

func TestZerologMetricsInvalidFileDoesNotPanic(t *testing.T) {
	// Provide a path to a directory that definitely does not exist
	invalidPath := "/does/not/exist/timestamps.log"

	// Create the metrics writer with timestamps enabled but an invalid path
	writer, err := NewZerologMetrics(true, invalidPath, "container-test")

	require.Error(t, err, "expected an error for invalid file path")
	require.NotNil(t, writer, "expected non-nil writer (fallback to mockWriter), got nil")

	_, isMock := writer.(*mockWriter)
	assert.True(t, isMock, "expected writer to be *mockWriter on file open failure")
}
