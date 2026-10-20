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
	"os"
	"path/filepath"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/pkg/unikontainers/initrd"
)

// FuzzInitrdStress takes arbitrary bytes as the image's initrd through both of urunc's ways of
// adding file mounts to it, and checks that a failed attempt leaves the file as it was and a
// successful one keeps every entry and the mounts, in an archive the kernel can unpack. It is
// off until the strict check "initrd-stress" is on: a malformed archive is not handled yet.
func FuzzInitrdStress(f *testing.F) {
	f.Add(fuzzBoot(f))
	f.Fuzz(func(t *testing.T, data []byte) {
		if !strictOn("initrd-stress") {
			return
		}
		dir := t.TempDir()
		src := filepath.Join(dir, "probe")
		if err := os.WriteFile(src, []byte("probe\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mounts := []specs.Mount{
			{Type: "bind", Source: src, Destination: "/etc/probe"},
			{Type: "bind", Source: src, Destination: "/data/conf/app.conf"},
		}
		for _, merge := range []bool{true, false} {
			path := filepath.Join(dir, "initrd")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			var err error
			if merge {
				err = initrd.MergeFileMountsIntoInitrd(path, mounts)
			} else {
				err = initrd.CopyFileMountsToInitrd(path, mounts)
			}
			result, _ := os.ReadFile(path)
			if err != nil {
				if !bytes.Equal(result, data) {
					t.Fatalf("merge=%v: the initrd changed though adding the mounts failed: %v", merge, err)
				}
				continue
			}
			before := map[string]bool{}
			for _, e := range initrdEntries(data) {
				before[e.name] = true
			}
			after := map[string]bool{}
			for _, e := range initrdEntries(result) {
				after[e.name] = true
			}
			for name := range before {
				if !after[name] {
					t.Fatalf("merge=%v: the entry %s is gone from the initrd", merge, name)
				}
			}
			if problems := initrdProblems(result, []string{"etc/probe", "data/conf/app.conf"}); len(problems) > 0 && len(before) > 0 {
				t.Fatalf("merge=%v: %v", merge, problems)
			}
		}
	})
}
