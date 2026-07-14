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
	"fmt"
	"os"
	"path/filepath"

	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
)

// PatchConfig reads config.json from bundlePath, applies fn to the spec,
// and writes it back atomically.
func PatchConfig(bundlePath string, fn func(*runtimespec.Spec) error) error {
	configPath := filepath.Join(bundlePath, "config.json")

	fi, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("stat config.json: %w", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config.json: %w", err)
	}

	var spec runtimespec.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("unmarshal config.json: %w", err)
	}

	if err := fn(&spec); err != nil {
		return err
	}

	patched, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config.json: %w", err)
	}

	return atomicWriteFile(configPath, patched, fi.Mode())
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmpDir := filepath.Dir(path)

	f, err := os.CreateTemp(tmpDir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}

	tmpName := f.Name()
	defer os.Remove(tmpName)

	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, path)
}
