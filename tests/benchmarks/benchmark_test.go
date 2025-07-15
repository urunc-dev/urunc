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

package main

import (
	"fmt"
	"testing"

	"github.com/urunc-dev/urunc/internal/constants"
	m "github.com/urunc-dev/urunc/internal/metrics"
)

func BenchmarkZerologWriter(b *testing.B) {
	var zerologWriter = m.NewZerologMetrics(constants.TimestampTargetFile)
	for i := 0; i < b.N; i++ {
		for j := 0; j < 20; j++ {
			zerologWriter.Capture(fmt.Sprintf("container%02d", i), fmt.Sprintf("TS%02d", j))
		}
	}
}

func BenchmarkMockWriter(b *testing.B) {
	var mockWriter = m.NewMockMetrics(constants.TimestampTargetFile)
	for i := 0; i < b.N; i++ {
		for j := 0; j < 20; j++ {
			mockWriter.Capture(fmt.Sprintf("container%02d", i), fmt.Sprintf("TS%02d", j))
		}
	}
}
