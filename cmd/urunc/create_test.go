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

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWaitForReexecReady(t *testing.T) {
	t.Parallel()

	require.NoError(t, waitForReexecReady(strings.NewReader(`""`)))
	require.EqualError(t, waitForReexecReady(strings.NewReader(`"CreateContainer hook failed"`)),
		"reexec setup failed: CreateContainer hook failed")
	require.ErrorContains(t, waitForReexecReady(strings.NewReader("")), "failed to read readiness")
}
