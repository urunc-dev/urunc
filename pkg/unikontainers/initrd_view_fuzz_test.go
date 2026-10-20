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
	"bytes"
	"io"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/cavaliergopher/cpio"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// initrdEntry is one entry of an initrd, in the order the kernel unpacks them.
type initrdEntry struct {
	name   string
	dir    bool
	file   bool
	offset int // where the archive that holds the entry starts in the initrd
}

// countReader counts the bytes read, to tell where a cpio archive ends.
type countReader struct {
	r io.Reader
	n int
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// initrdEntries lists every entry of every newc archive in data; an initrd may hold
// several, one after the other.
func initrdEntries(data []byte) []initrdEntry {
	var entries []initrdEntry
	for pos := 0; pos < len(data); {
		at := bytes.Index(data[pos:], []byte("0707"))
		if at < 0 {
			break
		}
		cr := &countReader{r: bytes.NewReader(data[pos+at:])}
		for rd := cpio.NewReader(cr); ; {
			h, err := rd.Next()
			if err != nil {
				break
			}
			mode := h.FileInfo().Mode()
			if name := strings.TrimPrefix(path.Clean("/"+h.Name), "/"); name != "" {
				entries = append(entries, initrdEntry{name: name, dir: mode.IsDir(), file: mode.IsRegular(), offset: pos + at})
			}
		}
		if cr.n == 0 {
			break
		}
		pos += at + cr.n
	}
	return entries
}

// initrdNoConflicts: a merged initrd never has an entry under a path that is a file in the archive.
func initrdNoConflicts(r *fuzzRun) []string {
	if !r.done() || r.rp.Type != "initrd" {
		return nil
	}
	data, err := os.ReadFile(r.initrdHost)
	if err != nil {
		return nil
	}
	files := map[string]bool{}
	for _, e := range initrdEntries(data) {
		for dir := path.Dir(e.name); dir != "."; dir = path.Dir(dir) {
			if files[dir] {
				return []string{"entry-under-a-file | " + e.name}
			}
		}
		if e.file {
			files[e.name] = true
		}
	}
	return nil
}

var plainPath = regexp.MustCompile(`^[A-Za-z0-9_./=:-]+$`)

// fileMountDests returns the destinations of the container's bind mounts of regular files,
// which urunc copies into the initrd.
func fileMountDests(mounts []specs.Mount) []string {
	var dests []string
	for _, m := range mounts {
		d := m.Destination
		if m.Type != "bind" || m.Source == "/proc" || path.Clean(d) != d || !path.IsAbs(d) || d == "/" || !plainPath.MatchString(d) {
			continue
		}
		if fi, err := os.Stat(m.Source); err == nil && fi.Mode().IsRegular() {
			dests = append(dests, strings.TrimPrefix(d, "/"))
		}
	}
	return dests
}

// initrdProblems checks an initrd that urunc added file mounts to against what the kernel needs to
// unpack it: every mount is in it, every entry's parent directory comes before it, and every archive
// starts on a 4-byte boundary.
func initrdProblems(data []byte, dests []string) []string {
	var problems []string
	entries := initrdEntries(data)
	seen, dirs := map[string]bool{}, map[string]bool{".": true}
	for _, e := range entries {
		if parent := path.Dir(e.name); !dirs[parent] {
			problems = append(problems, "parent-directory-missing | "+e.name)
			break
		}
		seen[e.name] = true
		if e.dir {
			dirs[e.name] = true
		}
	}
	for _, d := range dests {
		if !seen[d] {
			problems = append(problems, "file-mount-missing-from-initrd | "+d)
			break
		}
	}
	for _, e := range entries {
		if e.offset%4 != 0 {
			problems = append(problems, "archive-not-4-byte-aligned")
			break
		}
	}
	return problems
}
