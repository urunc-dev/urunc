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
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cavaliergopher/cpio"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// FuzzMergeFileMountsIntoInitrdPreservesOriginalOnError generalizes 5
// hand-picked corruption cases into a property: if the pre-write parse
// fails, the on-disk file must be byte-for-byte unchanged.

// fuzzWriteSource is writeSource (initrd_test.go), duplicated here rather
// than called across files. OSS-Fuzz/ClusterFuzzLite's Go build
// (go-118-fuzz-build) packages one target's containing file in isolation, so
// a helper defined in a sibling _test.go file is invisible to the generated
// harness, and rewrites this file's own *testing.T to its own look-alike
// type, so the helper takes a small interface instead of the concrete type.
func fuzzWriteSource(t interface {
	Helper()
	Fatalf(format string, args ...any)
}, dir, name, content string) string {
	t.Helper()
	filename := filepath.Join(dir, name)
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatalf("could not write source file: %v", err)
	}
	return filename
}

// fuzzCpio builds a newc archive from name/content pairs, as seed material.
func fuzzCpio(entries ...string) []byte {
	var buf bytes.Buffer
	w := cpio.NewWriter(&buf)
	for i := 0; i+1 < len(entries); i += 2 {
		_ = w.WriteHeader(&cpio.Header{Name: entries[i], Mode: 0o644, Size: int64(len(entries[i+1])), Inode: int64(i + 1)})
		_, _ = w.Write([]byte(entries[i+1]))
	}
	_ = w.Close()
	return buf.Bytes()
}

func FuzzMergeFileMountsIntoInitrdPreservesOriginalOnError(f *testing.F) {
	f.Add([]byte("not a cpio archive"), "/mounted")
	f.Add(fuzzCpio(), "/mounted")
	f.Add(fuzzCpio("./init", "#!/bin/sh"), "/etc/app/config")
	f.Add(fuzzCpio("./etc", "", "./etc/hosts", "old"), "/etc/hosts")

	f.Fuzz(func(t *testing.T, existing []byte, dest string) {
		// Engines reject relative mount destinations before urunc sees them.
		if !path.IsAbs(dest) {
			return
		}
		dir := t.TempDir()
		initrdPath := filepath.Join(dir, "initrd.cpio")
		if err := os.WriteFile(initrdPath, existing, 0o600); err != nil {
			t.Fatalf("could not seed initrd file: %v", err)
		}
		source := fuzzWriteSource(t, dir, "mounted", "new-content")

		err := MergeFileMountsIntoInitrd(initrdPath, []specs.Mount{
			{Type: "bind", Source: source, Destination: dest},
		})

		after, readErr := os.ReadFile(initrdPath)
		if readErr != nil {
			t.Fatalf("could not read back %s: %v", initrdPath, readErr)
		}

		if err != nil {
			// A partial write here corrupts the guest's initrd on the next boot.
			if !bytes.Equal(existing, after) {
				t.Fatalf("MergeFileMountsIntoInitrd(dest=%q) failed (%v) but still modified the file.\n  before (%d bytes): %x\n  after  (%d bytes): %x",
					dest, err, len(existing), existing, len(after), after)
			}
			return
		}

		// Success: the output must parse, and the last record for dest must
		// hold the mounted file's content (later records win on unpack).
		r := cpio.NewReader(bytes.NewReader(after))
		want := "./" + strings.TrimPrefix(path.Clean(dest), "/")
		var got []byte
		found := false
		for {
			hdr, err := r.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("merge (dest=%q) reported success but its output does not parse: %v", dest, err)
			}
			if hdr.Name == want {
				found = true
				got, _ = io.ReadAll(r)
			}
		}
		if !found || string(got) != "new-content" {
			t.Fatalf("merge (dest=%q) reported success but record %q holds %q (found=%v)", dest, want, got, found)
		}
	})
}
