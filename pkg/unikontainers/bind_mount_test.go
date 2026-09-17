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

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBindMount(t *testing.T) {
	t.Parallel()

	assert.True(t, isBindMount(specs.Mount{Type: "bind"}))
	assert.True(t, isBindMount(specs.Mount{Type: "rbind"}))
	// nerdctl and containerd spell a volume as type none with an rbind option.
	assert.True(t, isBindMount(specs.Mount{Type: "none", Options: []string{"rbind", "rprivate", "rw"}}))
	assert.True(t, isBindMount(specs.Mount{Options: []string{"bind"}}))
	assert.False(t, isBindMount(specs.Mount{Type: "tmpfs", Options: []string{"nosuid"}}))
	assert.False(t, isBindMount(specs.Mount{Type: "proc"}))

	// filterBindMounts keeps such volumes and re-roots them under the shared rootfs.
	dir := t.TempDir()
	mounts, err := filterBindMounts(dir, []specs.Mount{
		{Type: "none", Source: "/host/vol", Destination: "/data", Options: []string{"rbind", "rw"}},
		{Type: "tmpfs", Source: "tmpfs", Destination: "/tmp"},
	})
	require.NoError(t, err)
	require.Len(t, mounts, 1)
	assert.Equal(t, "/host/vol", mounts[0].Source)
	assert.Equal(t, containerRootfsMountPath+"/data", mounts[0].Destination)
}
