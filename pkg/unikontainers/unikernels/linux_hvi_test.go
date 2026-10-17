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
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestLinuxHviContainerBoot(t *testing.T) {
	t.Parallel()

	l := &Linux{
		App:           containerBootInit,
		Command:       "/bin/sh -c 'printf container-boot-ok'",
		Monitor:       "hvi",
		RootFsType:    "virtiofs",
		InitrdConf:    true,
		ContainerBoot: true,
	}
	cmdline, err := l.CommandString()
	require.NoError(t, err)

	assert.Contains(t, cmdline, "root=fs0 rw rootfstype=virtiofs")
	assert.Contains(t, cmdline, "rdinit=/init -- /bin/sh -c 'printf container-boot-ok'")
	// hvi has no virtio console, and a reset ends the hvi process.
	assert.NotContains(t, cmdline, "hvc0")
	assert.NotContains(t, cmdline, "URUNIT_POWEROFF")
	if runtime.GOARCH == "arm64" {
		assert.Contains(t, cmdline, "console=ttyAMA0")
	} else {
		assert.Contains(t, cmdline, "console=ttyS0")
	}

	// The urunit configuration is appended to the boot initrd.
	assert.Empty(t, l.MonitorCli().ExtraInitrd)
}

func TestLinuxHviMonitorBlockCli(t *testing.T) {
	t.Parallel()

	l := &Linux{Monitor: "hvi", Blk: []types.BlockDevParams{{ID: "rootfs", Source: "/dev/dm-3"}}}
	args := l.MonitorBlockCli()
	require.Len(t, args, 1)
	assert.Equal(t, "rootfs", args[0].ID)
	assert.Equal(t, "/dev/dm-3", args[0].Path)
}
