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
	Capture(containerID string, timestampID string)
}

type zerologMetrics struct {
	logger *zerolog.Logger
}

func (z *zerologMetrics) Capture(containerID string, timestampID string) {
	z.logger.Log().Str("containerID", containerID).Str("timestampID", timestampID).Msg("")
}

func NewZerologMetrics(enabled bool, target string) Writer {
	if enabled {
		file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return nil
		}
		logger := zerolog.New(file).Level(zerolog.InfoLevel).With().Timestamp().Logger()
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixNano
		return &zerologMetrics{
			logger: &logger,
		}
	}
	return &mockWriter{}
}

type mockWriter struct{}

func (m *mockWriter) Capture(_, _ string) {}

func NewMockMetrics(_ string) Writer {
	return &mockWriter{}
}
