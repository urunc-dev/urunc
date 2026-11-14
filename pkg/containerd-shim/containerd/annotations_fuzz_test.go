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

package containerd

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	imagespec "github.com/opencontainers/image-spec/specs-go/v1"
	runtimespec "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/internal/fuzzseed"
)

// FuzzFetchUruncAnnotations fetches the urunc annotations of a container with
// fixed labels and an arbitrary image manifest.
func FuzzFetchUruncAnnotations(f *testing.F) {
	// The manifest overrides the label, and other keys are dropped, even with the prefix but no dot.
	fuzzseed.Add(f, []byte(`{"annotations":{"`+labelKey+`":"manifest","`+uruncPrefix+`b":"2","com.urunc.c":"3","com.urunc.unikernel-c":"4"}}`))
	fuzzseed.Add(f, []byte(`{}`))
	fuzzseed.Add(f, []byte(`{"annotations":`))

	f.Fuzz(func(t *testing.T, manifest []byte) {
		got, err := fetch(ociManifest, manifest, &fakeStore{chunks: [][]byte{manifest}})

		var parsed imagespec.Manifest
		wantErr := json.Unmarshal(manifest, &parsed) != nil
		if (err != nil) != wantErr || (err != nil && got != nil) {
			t.Fatalf("got %v, error %v, want an error: %v", got, err, wantErr)
		}
		if err != nil {
			return
		}

		// The urunc label, then the urunc manifest annotations, which win.
		want := map[string]string{labelKey: "label"}
		for k, v := range parsed.Annotations {
			if strings.HasPrefix(k, uruncPrefix) {
				want[k] = v
			}
		}
		if !maps.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

// FuzzPatchConfigJSON patches two annotations into an arbitrary config.json.
func FuzzPatchConfigJSON(f *testing.F) {
	// An annotation that is already there keeps its value.
	fuzzseed.Add(f, []byte(`{"hostname":"h","mounts":[{"destination":"/d","type":"bind","source":"/s"}],"annotations":{"`+uruncPrefix+`a":"old","keep":"1"}}`))
	fuzzseed.Add(f, []byte(`{}`))
	fuzzseed.Add(f, []byte(`{`))
	bundle := f.TempDir()
	path := filepath.Join(bundle, "config.json")
	annotations := map[string]string{uruncPrefix + "a": "new", uruncPrefix + "b": "2"}

	f.Fuzz(func(t *testing.T, config []byte) {
		// A non-default mode, to check it is kept. Chmod because the umask applies to WriteFile.
		if err := os.WriteFile(path, config, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o640); err != nil { //nolint:gosec // a non-default mode, to check it is kept
			t.Fatal(err)
		}
		var spec runtimespec.Spec
		validSpec := json.Unmarshal(config, &spec) == nil

		patchErr := patchConfigJSON(bundle, annotations)

		patched, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if entries, _ := os.ReadDir(bundle); len(entries) != 1 {
			t.Fatalf("%d files left in the bundle", len(entries))
		}
		if !validSpec {
			if patchErr == nil || !bytes.Equal(patched, config) {
				t.Fatalf("invalid config.json: error %v, rewritten: %v", patchErr, !bytes.Equal(patched, config))
			}
			return
		}
		if patchErr != nil {
			t.Fatalf("patching a valid config.json: %v", patchErr)
		}
		if now, err := os.Stat(path); err != nil || now.Mode() != 0o640 {
			t.Fatalf("mode changed from 0640: %v, %v", now, err)
		}

		var out runtimespec.Spec
		if err := json.Unmarshal(patched, &out); err != nil {
			t.Fatalf("patched config.json is not a spec: %v", err)
		}
		// Missing annotations are added, existing ones keep their value.
		want := maps.Clone(annotations)
		maps.Copy(want, spec.Annotations)
		if !maps.Equal(out.Annotations, want) {
			t.Fatalf("annotations %v, want %v", out.Annotations, want)
		}
		// Nothing else changes, except fields this runtime-spec version does not know, which are dropped.
		spec.Annotations, out.Annotations = nil, nil
		before, _ := json.Marshal(spec)
		after, _ := json.Marshal(out)
		if !bytes.Equal(before, after) {
			t.Fatalf("config.json changed besides annotations:\n%s\n%s", before, after)
		}
	})
}
