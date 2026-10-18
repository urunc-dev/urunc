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

// Platform-neutral filesystem path helpers shared by the Linux orchestration
// engine and the darwin runner. Both are pure path/stat logic (no namespaces
// or mount syscalls), so they live here rather than being duplicated or forked
// per platform.

package unikontainers

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// resolveAgainstBase returns path unchanged when it is absolute, otherwise it
// resolves path relative to base (making base absolute first if needed).
func resolveAgainstBase(base string, path string) (string, error) {
	resolvedPath := path

	if !filepath.IsAbs(path) {
		baseAbs := base
		var err error

		if !filepath.IsAbs(base) {
			baseAbs, err = filepath.Abs(base)
			if err != nil {
				return "", fmt.Errorf("could not get absolute path of %s: %w", base, err)
			}
		}
		resolvedPath = filepath.Join(baseAbs, path)
	}

	return resolvedPath, nil
}

// fileExists reports whether fpath can be stat'd. unix.Stat is available on
// both Linux and darwin.
func fileExists(fpath string) bool {
	var fileInfo unix.Stat_t

	err := unix.Stat(fpath, &fileInfo)
	if err != nil {
		uniklog.Infof("Stat %s failed with: %v", fpath, err)
		return false
	}

	return true
}
