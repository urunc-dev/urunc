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
)

// FuzzGetUnikernelConfig feeds arbitrary bytes as the image's urunc.json.
// Whatever the content, the result must be either a config or an error.
func FuzzGetUnikernelConfig(f *testing.F) {
	// A valid config: linux, qemu and /app, base64 encoded.
	f.Add([]byte(`{"` + annotType + `":"bGludXg=","` + annotHypervisor + `":"cWVtdQ==","` + annotBinary + `":"L2FwcA=="}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		rootfs := t.TempDir()
		if err := os.WriteFile(filepath.Join(rootfs, uruncJSONFilename), data, 0600); err != nil {
			t.Fatal(err)
		}

		conf, err := GetUnikernelConfig("", &specs.Spec{Root: &specs.Root{Path: rootfs}})
		if (conf == nil) == (err == nil) {
			t.Fatalf("got config %v and error %v", conf, err)
		}
	})
}
