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

package unikontainers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/unix"
)

func TestMapRootfsPropagationFlag(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
		wantErr  bool
	}{
		{name: "rprivate", input: "rprivate", expected: unix.MS_PRIVATE | unix.MS_REC},
		{name: "private", input: "private", expected: unix.MS_PRIVATE},
		{name: "rslave", input: "rslave", expected: unix.MS_SLAVE | unix.MS_REC},
		{name: "slave", input: "slave", expected: unix.MS_SLAVE},
		{name: "rshared", input: "rshared", expected: unix.MS_SHARED | unix.MS_REC},
		{name: "shared", input: "shared", expected: unix.MS_SHARED},
		{name: "runbindable", input: "runbindable", expected: unix.MS_UNBINDABLE | unix.MS_REC},
		{name: "unbindable", input: "unbindable", expected: unix.MS_UNBINDABLE},
		{name: "unknown value returns error", input: "unknown", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := mapRootfsPropagationFlag(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, 0, got)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
