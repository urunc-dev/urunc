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
	"encoding/json"
	"maps"
	"strings"
	"testing"

	imagespec "github.com/opencontainers/image-spec/specs-go/v1"

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
		got, err := fetch(imagespec.MediaTypeImageManifest, manifest, &fakeStore{chunks: [][]byte{manifest}})

		var parsed imagespec.Manifest
		parseErr := json.Unmarshal(manifest, &parsed)
		if (err == nil) != (parseErr == nil) {
			t.Fatalf("error %v, but parsing the manifest gives %v", err, parseErr)
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
