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
	"bufio"
	"fmt"
	"strings"
	"testing"
	"unicode"
)

// getMountInfo hardcodes /proc/self/mountinfo, so matchMountInfoLine mirrors
// its matching loop (block.go:72-105) byte-for-byte on synthetic content
// instead. Keep them in sync by hand.
func matchMountInfoLine(mountinfoContent string, path string) (source, fsType, mountOptions string, found bool) {
	nonSpecialSources := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(mountinfoContent))

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, " - ")
		if len(parts) != 2 {
			continue // malformed synthetic line; not the property under test
		}

		preDash := strings.Fields(parts[0])
		if len(preDash) < 6 {
			continue
		}
		postDash := strings.Fields(parts[1])
		if len(postDash) < 2 {
			continue
		}
		if preDash[4] == path {
			source = postDash[1]
			fsType = postDash[0]
			mountOptions = preDash[5]
			found = true
			continue
		}
		if postDash[0] != postDash[1] {
			nonSpecialSources[postDash[1]] = struct{}{}
		}
	}

	if !found {
		return "", "", "", false
	}
	if _, ok := nonSpecialSources[source]; ok {
		return "", "", "", false
	}
	return source, fsType, mountOptions, true
}

// kernelEscapeMountPoint mirrors the kernel's octal-escaping of
// space/tab/newline/backslash in /proc/self/mountinfo (fs/proc_namespace.c).
func kernelEscapeMountPoint(path string) string {
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case ' ':
			b.WriteString(`\040`)
		case '\t':
			b.WriteString(`\011`)
		case '\n':
			b.WriteString(`\012`)
		case '\\':
			b.WriteString(`\134`)
		default:
			b.WriteByte(path[i])
		}
	}
	return b.String()
}

// FuzzGetMountInfoMatcher checks the mirrored matcher finds a present mount
// point once kernel escaping is accounted for. Reachable via
// tryContainerBlockRootfs(rootfs.go:230), so a rootfs path with a space is
// the real failure mode.
func FuzzGetMountInfoMatcher(f *testing.F) {
	f.Add("/var/lib/containers/storage/overlay/abc123/merged", "overlay", "overlay")
	f.Add("/run/containerd/io.containerd.runtime.v2.task/default/id/rootfs", "ext4", "/dev/sda1")

	f.Fuzz(func(t *testing.T, path, fsType, source string) {
		if !strictOn("mountinfo-matcher") {
			return
		}
		// fsType/source must not break the synthetic line's field structure
		// (harness artifact); path is NOT filtered -- spaces in it are the
		// point. (A missing \v here once caused a self-inflicted false positive.)
		for _, s := range []string{fsType, source} {
			if s == "" || strings.ContainsAny(s, "\\") || strings.IndexFunc(s, unicode.IsSpace) >= 0 {
				return
			}
		}
		if path == "" {
			return
		}

		escaped := kernelEscapeMountPoint(path)

		// Other unicode whitespace (\r \v \f) passes through unescaped and
		// would split the line the same way -- checked generally this time.
		if strings.IndexFunc(escaped, unicode.IsSpace) >= 0 {
			return
		}

		line := fmt.Sprintf("36 25 0:30 / %s rw,relatime shared:1 - %s %s rw",
			escaped, fsType, source)

		// getMountInfo splits on the literal " - " token; a field of exactly
		// "-" would add a second one -- a different bug, skipped here.
		if len(strings.Split(line, " - ")) != 2 {
			return
		}

		// The kernel octal-escapes space/tab/newline/backslash in
		// /proc/self/mountinfo, but getMountInfo compares the raw field
		// byte-for-byte, so an escaped mount point never matches.
		gotSource, gotFsType, _, found := matchMountInfoLine(line, path)
		if !found {
			t.Fatalf("mount point %q is present in mountinfo (as kernel-escaped %q) but getMountInfo's matcher did not find it:\n  line: %q",
				path, escaped, line)
		}
		if gotSource != source || gotFsType != fsType {
			t.Fatalf("getMountInfo matched mount point %q but returned "+
				"source=%q fsType=%q, want source=%q fsType=%q",
				path, gotSource, gotFsType, source, fsType)
		}
	})
}
