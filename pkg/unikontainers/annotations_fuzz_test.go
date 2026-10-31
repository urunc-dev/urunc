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
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/internal/fuzzseed"
)

// FuzzGetUnikernelConfig feeds arbitrary bytes as the image's urunc.json.
func FuzzGetUnikernelConfig(f *testing.F) {
	fuzzseed.QuietLogs(f) // every input logs the fallback to urunc.json
	for _, seed := range fuzzseed.Load[string]("FuzzGetUnikernelConfig") {
		fuzzseed.Add(f, []byte(seed))
	}

	rootfs := f.TempDir()

	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(filepath.Join(rootfs, uruncJSONFilename), data, 0600); err != nil {
			t.Fatal(err)
		}

		_, _ = GetUnikernelConfig("", &specs.Spec{Root: &specs.Root{Path: rootfs}})
	})
}
