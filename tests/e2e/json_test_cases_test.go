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

package urunce2etesting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTestCasesFromJSON(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantErr   bool
		wantLen   int
		checkCase func(t *testing.T, got []containerTestArgs)
	}{
		{
			name:    "valid single case",
			content: `[{"image":"img:latest","name":"tc","expectOut":"hello","seccomp":true}]`,
			wantLen: 1,
			checkCase: func(t *testing.T, got []containerTestArgs) {
				assert.Equal(t, "img:latest", got[0].Image)
				assert.Equal(t, "tc", got[0].Name)
				assert.Equal(t, "hello", got[0].ExpectOut)
				assert.True(t, got[0].Seccomp)
				assert.Nil(t, got[0].TestFunc)
			},
		},
		{
			name:    "empty array returns empty slice",
			content: `[]`,
			wantLen: 0,
		},
		{
			name:    "nil slices become empty slices",
			content: `[{"image":"i","name":"n","expectOut":""}]`,
			wantLen: 1,
			checkCase: func(t *testing.T, got []containerTestArgs) {
				assert.Equal(t, []int64{}, got[0].Groups)
				assert.Equal(t, []containerVolume{}, got[0].Volumes)
				assert.Equal(t, []string{}, got[0].SideContainers)
			},
		},
		{
			name: "volumes are decoded correctly",
			content: func() string {
				cases := []jsonTestCase{{
					Image:     "i",
					Name:      "n",
					ExpectOut: "",
					Volumes:   []containerVolume{{Source: "/src", Dest: "/dst"}},
				}}
				b, _ := json.Marshal(cases)
				return string(b)
			}(),
			wantLen: 1,
			checkCase: func(t *testing.T, got []containerTestArgs) {
				require.Len(t, got[0].Volumes, 1)
				assert.Equal(t, "/src", got[0].Volumes[0].Source)
				assert.Equal(t, "/dst", got[0].Volumes[0].Dest)
			},
		},
		{
			name:    "file not found returns error",
			content: "", // signal to use a non-existent path
			wantErr: true,
		},
		{
			name:    "invalid JSON returns error",
			content: `{not valid json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var path string
			if tt.wantErr && tt.content == "" {
				path = filepath.Join(t.TempDir(), "nonexistent.json")
			} else {
				path = filepath.Join(t.TempDir(), "cases.json")
				require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o600))
			}

			got, err := loadTestCasesFromJSON(path)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
			if tt.checkCase != nil {
				tt.checkCase(t, got)
			}
		})
	}
}

func TestOptionalJSONTestCases(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		wantLen int
	}{
		{
			name: "absent file returns empty slice",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "missing.json")
			},
			wantLen: 0,
		},
		{
			name: "valid file returns cases",
			setup: func(t *testing.T) string {
				cases := []jsonTestCase{{Image: "img:latest", Name: "tc", ExpectOut: "ok"}}
				data, _ := json.Marshal(cases)
				path := filepath.Join(t.TempDir(), "cases.json")
				require.NoError(t, os.WriteFile(path, data, 0o600))
				return path
			},
			wantLen: 1,
		},
		{
			name: "invalid JSON returns empty slice",
			setup: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "cases.json")
				require.NoError(t, os.WriteFile(path, []byte("{bad json"), 0o600))
				return path
			},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := tt.setup(t)
			got := optionalJSONTestCases(path)
			assert.Len(t, got, tt.wantLen)
		})
	}
}
