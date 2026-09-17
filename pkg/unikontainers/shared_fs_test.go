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
)

func TestVirtiofsIDTranslations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		owners     []bindMountOwner
		gUID, gGID uint32
		want       []string
	}{
		{
			name:   "no bind mounts",
			owners: nil,
			gUID:   1000, gGID: 1000,
			want: nil,
		},
		{
			name:   "root-owned volume is left alone",
			owners: []bindMountOwner{{uid: 0, gid: 0}},
			gUID:   1000, gGID: 1000,
			want: nil,
		},
		{
			name:   "non-root owner maps the non-root container user onto it",
			owners: []bindMountOwner{{uid: 1000, gid: 1000}},
			gUID:   500, gGID: 500,
			want: []string{
				"--translate-uid", "map:500:1000:1",
				"--translate-gid", "map:500:1000:1",
			},
		},
		{
			name:   "root container user needs no translation",
			owners: []bindMountOwner{{uid: 1000, gid: 1000}},
			gUID:   0, gGID: 0,
			want: nil,
		},
		{
			name:   "owner equal to the container user needs no shift",
			owners: []bindMountOwner{{uid: 1000, gid: 1000}},
			gUID:   1000, gGID: 1000,
			want: nil,
		},
		{
			name: "first non-root owner wins across volumes",
			owners: []bindMountOwner{
				{uid: 0, gid: 0},
				{uid: 1000, gid: 2000},
				{uid: 3000, gid: 4000},
			},
			gUID: 500, gGID: 600,
			want: []string{
				"--translate-uid", "map:500:1000:1",
				"--translate-gid", "map:600:2000:1",
			},
		},
		{
			name: "uid and gid are chosen independently",
			owners: []bindMountOwner{
				{uid: 0, gid: 2000},
				{uid: 1000, gid: 0},
			},
			gUID: 500, gGID: 600,
			want: []string{
				"--translate-uid", "map:500:1000:1",
				"--translate-gid", "map:600:2000:1",
			},
		},
		{
			name: "a non-root uid still maps when the container gid is root",
			owners: []bindMountOwner{
				{uid: 1000, gid: 2000},
			},
			gUID: 500, gGID: 0,
			want: []string{
				"--translate-uid", "map:500:1000:1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := virtiofsIDTranslations(tt.owners, tt.gUID, tt.gGID)
			assert.Equal(t, tt.want, got)
		})
	}
}
