//go:build linux
// +build linux

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

package initrd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cavaliergopher/cpio"
)

// containerBootPayloadFiles maps the minimal container-boot payload into an
// existing initrd. /init prepares the standard virtual filesystems and either
// executes directly from the initramfs or mounts a block rootfs and switches
// into it. BusyBox supplies the early-boot utilities without relying on the
// container image. urunit-agent preserves exec support after the handoff.
var containerBootPayloadFiles = []struct {
	dest, src string
	optional  bool
}{
	{dest: "/init", src: "init"},
	{dest: "/vz-init", src: "vz-init", optional: true},
	// Must be named exactly "busybox": busybox only dispatches argv[1] as the
	// applet (e.g. `busybox sh`) when argv[0]'s basename is "busybox". A renamed
	// binary makes every invocation "applet not found" (exit 127).
	{dest: "/busybox", src: "busybox"},
	// urunit is PID 1 once the init scripts hand over: it runs the image's
	// entrypoint as its child so that the entrypoint exiting reaps, syncs,
	// unmounts and powers the VM off, rather than panicking the kernel with
	// "Attempted to kill init!" and leaving a machine nobody can stop.
	//
	// Optional so that an initrd built before this still boots -- the scripts
	// fall back to exec'ing the entrypoint directly when it is absent.
	{dest: "/urunit", src: "urunit", optional: true},
	{dest: "/urunit-agent", src: "urunit-agent"},
}

// AugmentInitrdForContainerBoot appends the generic container-boot payload to
// an existing uncompressed cpio-newc initrd. The original archive remains
// intact; the appended /init becomes the early userspace entrypoint that hands
// control to the OCI process. Optional payload files may be omitted.
func AugmentInitrdForContainerBoot(initrdPath, payloadDir string) error {
	f, err := os.OpenFile(initrdPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open initrd %s for container-boot augmentation: %w", initrdPath, err)
	}
	defer f.Close()

	w := cpio.NewWriter(f)
	for _, pf := range containerBootPayloadFiles {
		full := filepath.Join(payloadDir, pf.src)
		if _, statErr := os.Stat(full); statErr != nil {
			if pf.optional {
				continue
			}
			return fmt.Errorf("container-boot payload file missing: %s: %w", full, statErr)
		}
		if err := CopyFileToInitrd(w, full, pf.dest); err != nil {
			return fmt.Errorf("failed to append container-boot payload %s: %w", pf.dest, err)
		}
	}

	return w.Close()
}
