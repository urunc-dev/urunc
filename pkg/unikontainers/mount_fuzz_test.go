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
)

// FuzzSplitMountOptions: splitMountOptions sorts each option into a
// propagation flag, a VFS bitmask bit, or an opaque containerd option --
// none may vanish (that weakens the sandbox silently).
//
// Also asserts the exact VFS bitmask (last-writer-wins per bit): mutation
// testing proved that necessary -- flipping the implementation's clear to a
// set went undetected without it, meaning a dropped "nosuid"/"ro" would too.
func FuzzSplitMountOptions(f *testing.F) {
	f.Add("ro", "nosuid", "rprivate", true)
	f.Add("rw", "size=65536k", "", false)
	f.Add("nodev", "noexec", "slave", true)
	f.Add("", "", "", false)
	f.Add("relatime", "norelatime", "mode=755", false)

	f.Fuzz(func(t *testing.T, o1, o2, o3 string, isBind bool) {
		options := []string{o1, o2, o3}

		containerdOpts, propagation, vfsFlags := splitMountOptions(options, isBind)

		// Last-writer-wins per flag bit (see comment above).
		type lastRef struct {
			clear bool
			opt   string
		}
		lastFor := map[uintptr]lastRef{}
		for _, o := range options {
			if _, err := mapRootfsPropagationFlag(o); err == nil {
				continue // consumed as propagation, never folded into the mask
			}
			flag, clear, err := mapVFSFlag(o)
			if err != nil {
				continue
			}
			lastFor[flag] = lastRef{clear: clear, opt: o}
		}
		for flag, ref := range lastFor {
			got := vfsFlags&flag != 0
			want := !ref.clear
			if got != want {
				verb := "set"
				if ref.clear {
					verb = "cleared"
				}
				t.Fatalf("splitMountOptions(%q, isBind=%v) returned VFS mask %#x, but the last "+
					"option affecting flag %#x was %q, which should have %s it (bit=%v want=%v). "+
					"This mask goes to mount(2), so a wrong bit means the mount silently gets "+
					"different protection flags than the spec asked for.",
					options, isBind, vfsFlags, flag, ref.opt, verb, got, want)
			}
		}

		inContainerd := make(map[string]int, len(containerdOpts))
		for _, o := range containerdOpts {
			inContainerd[o]++
		}

		wantPropagation := 0
		for _, o := range options {
			if _, err := mapRootfsPropagationFlag(o); err == nil {
				wantPropagation++
				// A propagation flag is consumed into the propagation slice and
				// must not also be forwarded to containerd.
				continue
			}
			if _, _, err := mapVFSFlag(o); err == nil && isBind {
				// For bind mounts a VFS flag is folded into the bitmask and
				// applied separately, so it is intentionally not forwarded.
				continue
			}
			// Everything else has no other bucket: it must reach containerd.
			if inContainerd[o] == 0 {
				t.Fatalf("splitMountOptions(%q, isBind=%v) dropped option %q: it is "+
					"not a propagation flag, it is not a VFS flag being folded into "+
					"the bitmask, and it does not appear in the containerd options "+
					"either.\n  containerdOpts=%q propagation=%v",
					options, isBind, o, containerdOpts, propagation)
			}
			inContainerd[o]--
		}

		if len(propagation) != wantPropagation {
			t.Fatalf("splitMountOptions(%q, isBind=%v) returned %d propagation "+
				"flag(s) but %d of the options are propagation flags",
				options, isBind, len(propagation), wantPropagation)
		}
	})
}

// The VFS and propagation tables must stay disjoint: splitMountOptions checks
// propagation first, so an overlap would silently drop the VFS flag.
func TestMountFlagTablesDisjoint(t *testing.T) {
	for _, opt := range []string{
		"ro", "rw", "nosuid", "suid", "nodev", "dev", "noexec", "exec",
		"noatime", "atime", "nodiratime", "diratime", "relatime", "norelatime",
		"strictatime", "nostrictatime", "nosymfollow", "symfollow", "sync",
		"async", "dirsync", "mand", "nomand", "silent", "loud", "lazytime",
		"nolazytime", "iversion", "noiversion",
		"rprivate", "private", "rslave", "slave", "rshared", "shared",
		"runbindable", "unbindable",
	} {
		flag, _, vfsErr := mapVFSFlag(opt)
		_, propErr := mapRootfsPropagationFlag(opt)
		if vfsErr == nil && propErr == nil {
			t.Fatalf("%q is accepted by both mapVFSFlag and mapRootfsPropagationFlag", opt)
		}
		if vfsErr != nil && propErr != nil {
			t.Fatalf("%q is accepted by neither table; update this list", opt)
		}
		if vfsErr == nil && flag == 0 {
			t.Fatalf("mapVFSFlag(%q) returned a zero bitmask", opt)
		}
	}
}
