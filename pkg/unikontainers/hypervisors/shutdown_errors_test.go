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
	"errors"
	"fmt"
	"testing"
)

func TestShutdownStageErrors(t *testing.T) {
	t.Parallel()

	stages := []error{
		ErrShutdownConnect,
		ErrShutdownGreeting,
		ErrShutdownHandshake,
		ErrShutdownCommand,
		ErrShutdownRefused,
	}

	seen := make(map[string]bool, len(stages))
	for _, stage := range stages {
		if stage == nil {
			t.Fatal("stage error is nil")
		}
		if seen[stage.Error()] {
			t.Fatalf("duplicate stage message %q", stage.Error())
		}
		seen[stage.Error()] = true
	}

	wrapped := fmt.Errorf("%w: dial unix: no such file", ErrShutdownConnect)
	if !errors.Is(wrapped, ErrShutdownConnect) {
		t.Fatal("wrapped error does not match its stage")
	}
	if errors.Is(wrapped, ErrShutdownCommand) {
		t.Fatal("wrapped error matches the wrong stage")
	}
}
