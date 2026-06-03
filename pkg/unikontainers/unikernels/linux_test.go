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

func TestLinuxMonitorBlockCli(t *testing.T) {
	t.Parallel()

	blocks := []types.BlockDevParams{
		{ID: "rootfs", Source: "/host/rootfs.img"},
		{ID: "vol0", Source: "/host/vol0.img"},
	}

	tests := []struct {
		name    string
		monitor string
		blk     []types.BlockDevParams
		want    []types.MonitorBlockArgs
	}{
		{
			name:    "no blocks returns nil",
			monitor: "qemu",
			blk:     nil,
			want:    nil,
		},
		{
			name:    "qemu emits ExactArgs without FC prefix",
			monitor: "qemu",
			blk:     blocks,
			want: []types.MonitorBlockArgs{
				{ExactArgs: " -device virtio-blk-pci,serial=rootfs,drive=rootfs" +
					" -drive format=raw,if=none,id=rootfs,file=/host/rootfs.img"},
				{ExactArgs: " -device virtio-blk-pci,serial=vol0,drive=vol0" +
					" -drive format=raw,if=none,id=vol0,file=/host/vol0.img"},
			},
		},
		{
			name:    "firecracker prefixes every ID with FC",
			monitor: "firecracker",
			blk:     blocks,
			want: []types.MonitorBlockArgs{
				{ID: "FCrootfs", Path: "/host/rootfs.img"},
				{ID: "FCvol0", Path: "/host/vol0.img"},
			},
		},
		{
			name:    "unknown monitor returns nil",
			monitor: "hvt",
			blk:     blocks,
			want:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := &Linux{Monitor: tc.monitor, Blk: tc.blk}
			assert.Equal(t, tc.want, l.MonitorBlockCli())
		})
	}
}
