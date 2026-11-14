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

// Package fuzzseed adds the seeds of a fuzz target and, for the ClusterFuzzLite
// build, which ignores f.Add, writes them to files.
package fuzzseed

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Add is f.Add for a single-[]byte target, the only shape libFuzzer keeps intact. When
// URUNC_SEED_DIR is set it also writes the seed there in the "go test fuzz v1" format.
func Add(f *testing.F, seed []byte) {
	f.Add(seed)
	dir := os.Getenv("URUNC_SEED_DIR")
	if dir == "" {
		return
	}
	file := fmt.Sprintf("go test fuzz v1\n[]byte(%q)\n", seed)
	name := fmt.Sprintf("%x", sha256.Sum256([]byte(file)))
	if err := os.WriteFile(filepath.Join(dir, name), []byte(file), 0o600); err != nil { //nolint:gosec // name is a hash
		f.Fatal(err)
	}
}
