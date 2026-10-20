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

// Guest cmdline encoding: CommandString space-joins spec.Process.Env entries
// (linux.go:111) with no escaping; it is reached for any non-urunit Linux guest.

// kernelFields splits like the kernel (kernel/params.c: ASCII space/tab
// only), NOT like strings.Fields, which is unicode-aware and over-splits.
func kernelFields(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t'
	})
}

// FuzzLinuxCmdlineEnvEncoding: an env entry must survive as exactly one
// kernel parameter. Measured: "A=1 root=/dev/sda1" silently redirects the
// guest's root device, since the kernel honours the LAST root=.
func FuzzLinuxCmdlineEnvEncoding(f *testing.F) {
	f.Add("PATH=/usr/bin", "block")
	f.Add("GREETING=hello world", "block")
	f.Add("A=1 root=/dev/sda1", "block")
	f.Add("MSG=x init=/bin/sh", "initrd")

	f.Fuzz(func(t *testing.T, envEntry, rootfsType string) {
		if !strictOn("linux-cmdline") {
			return
		}
		// Input realism: these are the rootfs types Linux guests actually
		// support; anything else exercises the harness, not the encoder.
		switch rootfsType {
		case "block", "initrd", "9pfs", "virtiofs":
		default:
			return
		}
		// An entry with no "=" isn't something the OCI runtime would hand over.
		if !strings.Contains(envEntry, "=") {
			return
		}

		l := newLinux()
		err := l.Init(types.UnikernelParams{
			// App must not contain "urunit", or InitrdConf flips true and the
			// env entries are not written to the cmdline at all (linux.go:248).
			CmdLine: []string{"/sbin/myinit"},
			EnvVars: []string{envEntry},
			Monitor: "qemu",
			Rootfs:  types.RootfsParams{Type: rootfsType},
		})
		if err != nil {
			return
		}
		if l.InitrdConf {
			return // urunit path -- covered by FuzzBuildUrunitConfig instead
		}

		out, err := l.CommandString()
		if err != nil {
			return
		}

		// Must appear as one contiguous parameter (matches kernelFields above).
		// Two earlier strings.Fields-based versions of this check both
		// mis-modelled the kernel's own splitting rule and gave false results.
		if strings.ContainsAny(envEntry, " \t") {
			t.Fatalf("env entry %q was encoded into the guest kernel command line as multiple kernel parameters instead of 1. The kernel splits its command line on whitespace and honours the LAST occurrence of a repeated parameter, so an ordinary env value can silently override a boot parameter urunc set earlier (root=, init=, console=, panic=).\n  cmdline: %s",
				envEntry, out)
		}

		// No whitespace in the entry: it must survive verbatim as one field.
		found := false
		for _, field := range kernelFields(out) {
			if field == envEntry {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("env entry %q did not survive encoding into the kernel command "+
				"line as a single parameter, and it contains no ASCII space or tab -- "+
				"this is "+
				"not the known whitespace-splitting shape:\n  cmdline: %s", envEntry, out)
		}
	})
}
