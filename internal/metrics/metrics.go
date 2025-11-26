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
	"os"

	"github.com/rs/zerolog"
)

type Writer interface {
	Capture(id TimestampID)
	SetLoggerContainerID(containerID string)
}

type zerologMetrics struct {
	logger      *zerolog.Logger
	containerID string
}

func (z *zerologMetrics) Capture(id TimestampID) {
	if id >= TimestampCount {
		z.logger.Log().
			Str("containerID", z.containerID).
			Uint("timestampID_invalid", uint(id)).
			Msg("invalid timestamp ID")
		return
	}
	meta := Timestamps[id]
	z.logger.Log().
		Str("containerID", z.containerID).
		Str("timestampID", meta.LegacyID).
		Str("timestampName", meta.Name).
		Int("timestampOrder", meta.Order).
		Msg("")
}

func (z *zerologMetrics) SetLoggerContainerID(containerID string) {
	z.containerID = containerID
}

// NewZerologMetrics creates a Writer that logs timestamps for a single container
func NewZerologMetrics(enabled bool, target string, containerID string) Writer {
	if enabled {
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixNano
		file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return nil
		}
		logger := zerolog.New(file).Level(zerolog.InfoLevel).With().Timestamp().Logger()
		return &zerologMetrics{
			logger:      &logger,
			containerID: containerID,
		}
	}
	return &mockWriter{}
}

type mockWriter struct{}

// Capture is a no-op used in tests where metrics are disabled
func (m *mockWriter) Capture(_ TimestampID) {
	// no-op
}

// SetLoggerContainerID is a no-op in tests where metrics are disabled
func (m *mockWriter) SetLoggerContainerID(_ string) {
	// no-op
}

func NewMockMetrics(_ string) Writer {
	return &mockWriter{}
}
