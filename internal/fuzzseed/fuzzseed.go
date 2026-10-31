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

// Package fuzzseed holds the seeds of the fuzz targets, in seeds.toml, adds them to a target and,
// for the nightly run, writes them to files. It also has what the targets share.
package fuzzseed

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/sirupsen/logrus"
)

//go:embed seeds.toml
var seedFile string

// seedTables is seeds.toml, parsed once: a libFuzzer build calls the fuzz function for every input.
var seedTables = sync.OnceValues(func() (map[string]toml.Primitive, toml.MetaData) {
	var tables map[string]toml.Primitive
	md, err := toml.Decode(seedFile, &tables)
	if err != nil {
		panic(err)
	}
	return tables, md
})

// Load returns the seeds of the target, as the rows of its table in seeds.toml. It panics on a
// table that is missing or does not fit T, as the file is part of the source.
func Load[T any](target string) []T {
	tables, md := seedTables()
	var rows []T
	if err := md.PrimitiveDecode(tables[target], &rows); err != nil || len(rows) == 0 {
		panic(fmt.Sprintf("no usable seeds for %s in seeds.toml: %v", target, err))
	}
	return rows
}

// Add is f.Add. With URUNC_SEED_DIR set it also writes the seed there, as the files go test
// reads them, because libFuzzer ignores f.Add.
func Add(f *testing.F, args ...any) {
	f.Add(args...)
	dir := os.Getenv("URUNC_SEED_DIR")
	if dir == "" {
		return
	}
	var file strings.Builder
	file.WriteString("go test fuzz v1\n")
	for _, a := range args {
		switch v := a.(type) {
		case string:
			fmt.Fprintf(&file, "string(%q)\n", v)
		case []byte:
			fmt.Fprintf(&file, "[]byte(%q)\n", v)
		default:
			fmt.Fprintf(&file, "%T(%v)\n", v, v)
		}
	}
	name := fmt.Sprintf("%x", sha256.Sum256([]byte(file.String())))
	if err := os.WriteFile(filepath.Join(dir, name), []byte(file.String()), 0o600); err != nil { //nolint:gosec // name is a hash
		f.Fatal(err)
	}
}

// QuietLogs silences urunc's logger for the target: it logs on every input.
func QuietLogs(f *testing.F) {
	logrus.SetOutput(io.Discard)
	f.Cleanup(func() { logrus.SetOutput(os.Stderr) })
}
