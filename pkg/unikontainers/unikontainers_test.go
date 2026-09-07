//go:build linux

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
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestConfineBlockSources(t *testing.T) {
	tests := []struct {
		name     string
		blocks   []types.BlockDevParams
		expected []string
	}{
		{
			name:     "explicit block image as the guest rootfs is confined",
			blocks:   []types.BlockDevParams{{ID: "rootfs", Source: "/rootfs.ext2", IsExplicit: true}},
			expected: []string{containerRootfsMountPath + "/rootfs.ext2"},
		},
		{
			name:     "container rootfs as a block device keeps the device path",
			blocks:   []types.BlockDevParams{{ID: "rootfs", Source: "/dev/mapper/snap", IsExplicit: false}},
			expected: []string{"/dev/mapper/snap"},
		},
		{
			name: "block volumes from the container mounts keep the device path",
			blocks: []types.BlockDevParams{
				{ID: "rootfs", Source: "/dev/mapper/snap", IsExplicit: false},
				{ID: "vol1", Source: "/dev/loop3", IsExplicit: false},
			},
			expected: []string{"/dev/mapper/snap", "/dev/loop3"},
		},
		{
			name:     "block image from the annotation without a guest rootfs is confined",
			blocks:   []types.BlockDevParams{{ID: "annot_vol", Source: "/data/vol.ext2", IsExplicit: true}},
			expected: []string{containerRootfsMountPath + "/data/vol.ext2"},
		},
		{
			name:     "parent references are clamped under the container rootfs",
			blocks:   []types.BlockDevParams{{ID: "rootfs", Source: "../../etc/shadow", IsExplicit: true}},
			expected: []string{containerRootfsMountPath + "/etc/shadow"},
		},
		{
			name:     "no block devices",
			blocks:   nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := confineBlockSources(tt.blocks)
			require.NoError(t, err)

			var sources []string
			for _, b := range got {
				sources = append(sources, b.Source)
			}
			assert.Equal(t, tt.expected, sources)
		})
	}
}

// The paths come from the image's annotations, so every one of them has to end
// up under the container rootfs mount, no matter how it is written.
func TestConfineToContainerRootfs(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
		wantErr  bool
	}{
		{name: "empty path stays empty", path: "", expected: ""},
		{name: "absolute path", path: "/unikernel/kernel", expected: containerRootfsMountPath + "/unikernel/kernel"},
		{name: "relative path", path: "unikernel/kernel", expected: containerRootfsMountPath + "/unikernel/kernel"},
		{name: "parent references are clamped", path: "/../../etc/passwd", expected: containerRootfsMountPath + "/etc/passwd"},
		{name: "root of the image", path: "/", expected: containerRootfsMountPath},
		{name: "comma is rejected", path: "/img,readonly=on", wantErr: true},
		{name: "equals is rejected", path: "/img=evil", wantErr: true},
		{name: "space is rejected", path: "/foo bar", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := confineToContainerRootfs(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
