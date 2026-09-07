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
	"strings"
	"testing"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// MonitorBlockCli dispatches on the monitor family and returns nil for anything
// it does not recognise, so a monitor missing from the switch gets no block
// devices at all -- while CommandString still emits root=/dev/vda for a
// block rootfs. The guest then boots with a root= that has nothing behind it,
// which is how this surfaced for hvi: vmi-init could not mount the rootfs, fell
// through to exec'ing the entrypoint from the initramfs, and exited 127.
//
// This test pins the pairing rather than the hvi case alone: every monitor that
// a block rootfs can be built for must return a device for it.
func TestMonitorBlockCliCoversBlockRootfsMonitors(t *testing.T) {
	t.Parallel()

	const rootfsImg = "/run/urunc/rootfs.img"

	for _, monitor := range []string{"qemu", "firecracker", "cloud-hypervisor", "hvi"} {
		t.Run(monitor, func(t *testing.T) {
			t.Parallel()

			l := &Linux{
				Monitor:    monitor,
				RootFsType: "block",
				Blk: []types.BlockDevParams{
					{ID: "rootfs", Source: rootfsImg, MountPoint: "/"},
				},
			}

			cmdline, err := l.CommandString()
			if err != nil {
				t.Fatalf("CommandString: %v", err)
			}
			if !strings.Contains(cmdline, "root=/dev/vda") {
				t.Fatalf("expected root=/dev/vda in %q", cmdline)
			}

			blkArgs := l.MonitorBlockCli()
			if len(blkArgs) != 1 {
				t.Fatalf("expected 1 block device for %s, got %d", monitor, len(blkArgs))
			}
			// qemu is passed a whole argument vector; the others a path.
			got := blkArgs[0].Path
			if got == "" {
				got = strings.Join(blkArgs[0].ExactArgs, " ")
			}
			if !strings.Contains(got, rootfsImg) {
				t.Fatalf("expected %q to carry %q", got, rootfsImg)
			}
		})
	}
}

// An image with no block devices must not fabricate one for any monitor.
func TestMonitorBlockCliEmptyWithoutDevices(t *testing.T) {
	t.Parallel()

	for _, monitor := range []string{"qemu", "firecracker", "cloud-hypervisor", "hvi"} {
		l := &Linux{Monitor: monitor}
		if got := l.MonitorBlockCli(); len(got) != 0 {
			t.Fatalf("%s: expected no block devices, got %v", monitor, got)
		}
	}
}
