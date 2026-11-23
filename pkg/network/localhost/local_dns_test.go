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

package localhost

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTemp(t *testing.T, dir, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "resolv.conf")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644)) //nolint: gosec
	return path
}

func TestDetect(t *testing.T) {
	tmp := t.TempDir()

	tests := []struct {
		name       string
		dir        string // subdirectory holding resolv.conf
		content    string
		virtIP     string
		wantNil    bool
		wantLoIP   string
		wantVirtIP string
	}{
		{
			name:       "docker embedded resolver",
			dir:        "var/lib/docker/containers/abc",
			content:    "nameserver 127.0.0.11\noptions ndots:0\n",
			virtIP:     "192.168.100.100",
			wantLoIP:   "127.0.0.11",
			wantVirtIP: "192.168.100.100",
		},
		{
			name:       "loopback after public nameserver",
			dir:        "mixed",
			content:    "nameserver 8.8.8.8\nnameserver 127.0.0.11\n",
			virtIP:     "192.168.100.100",
			wantLoIP:   "127.0.0.11",
			wantVirtIP: "192.168.100.100",
		},
		{
			name:    "no loopback nameserver",
			dir:     "plain",
			content: "search example.com\nnameserver 8.8.8.8\n",
			virtIP:  "192.168.100.100",
			wantNil: true,
		},
		{
			name:    "garbage lines are ignored",
			dir:     "garbage",
			content: "# a comment\nnameserver\nnot-an-ip\n",
			virtIP:  "192.168.100.100",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, filepath.Join(tmp, tt.dir), tt.content)
			fwd, err := Detect(path, tt.virtIP)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, fwd, "Detect() should not return a Forwarder")
				return
			}
			require.NotNil(t, fwd, "Detect() should return a Forwarder")
			assert.Equal(t, tt.wantLoIP, fwd.LoIP.String())
			assert.Equal(t, tt.wantVirtIP, fwd.VirtIP.String())
			assert.Equal(t, path, fwd.ResolvConf)
			assert.Equal(t, reflect.ValueOf(dockerRules).Pointer(), reflect.ValueOf(fwd.custom).Pointer(), "custom rules should be dockerRules")
		})
	}

	t.Run("empty path", func(t *testing.T) {
		fwd, err := Detect("", "192.168.100.100")
		assert.NoError(t, err)
		assert.Nil(t, fwd)
	})

	t.Run("missing file", func(t *testing.T) {
		fwd, err := Detect(filepath.Join(tmp, "does/not/exist"), "192.168.100.100")
		assert.Error(t, err)
		assert.Nil(t, fwd)
	})
}

func TestRewriteResolvConf(t *testing.T) {
	tmp := t.TempDir()
	content := "search example.com\nnameserver 127.0.0.11\nnameserver 8.8.8.8\noptions ndots:0\n"
	path := writeTemp(t, filepath.Join(tmp, "var/lib/docker/containers/abc"), content)

	fwd, err := Detect(path, "192.168.100.100")
	require.NoError(t, err)
	require.NotNil(t, fwd)
	require.NoError(t, fwd.RewriteResolvConf())

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	want := "search example.com\nnameserver 192.168.100.100\nnameserver 8.8.8.8\noptions ndots:0\n"
	assert.Equal(t, want, string(got), "only loopback nameservers should be rewritten")
}
