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

package unikernels

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// BuildUrunitConfig is the body of (*Linux).buildUrunitConfig lifted out for
// runners outside this package. The expected strings are what the method
// rendered before the export, so both have to keep producing them.
func TestBuildUrunitConfig(t *testing.T) {
	t.Parallel()

	env := []string{"PATH=/usr/bin", "FOO=bar"}
	proc := types.ProcessConfig{UID: 1000, GID: 100, WorkDir: "/work"}
	blk := []types.BlockDevParams{
		{ID: "rootfs", MountPoint: "/"},
		{ID: "vol0", MountPoint: "/data"},
	}

	tests := []struct {
		name    string
		monitor string
		want    string
	}{
		{
			name:    "firecracker prefixes the block ids",
			monitor: "firecracker",
			want:    "UES\nPATH=/usr/bin\nFOO=bar\nUEE\nUCS\nUID:1000\nGID:100\nWD:/work\nUCE\nUBS\nID:FCvol0\nMP:/data\nUBE\n",
		},
		{
			name:    "other monitors keep the block ids",
			monitor: "qemu",
			want:    "UES\nPATH=/usr/bin\nFOO=bar\nUEE\nUCS\nUID:1000\nGID:100\nWD:/work\nUCE\nUBS\nID:vol0\nMP:/data\nUBE\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			l := &Linux{Env: env, ProcConfig: proc, Blk: blk, Monitor: tc.monitor}
			assert.Equal(t, tc.want, BuildUrunitConfig(env, proc, blk, tc.monitor))
			assert.Equal(t, tc.want, l.buildUrunitConfig())
		})
	}

	t.Run("empty environment and no block mounts", func(t *testing.T) {
		t.Parallel()

		want := "UES\nUEE\nUCS\nUID:0\nGID:0\nWD:\nUCE\nUBS\nUBE\n"
		assert.Equal(t, want, BuildUrunitConfig(nil, types.ProcessConfig{}, nil, "firecracker"))
		assert.Equal(t, want, (&Linux{Monitor: "firecracker"}).buildUrunitConfig())
	})
}
