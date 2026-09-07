//go:build darwin
// +build darwin

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
	"fmt"

	"github.com/opencontainers/runtime-spec/specs-go"
)

var ErrCopyDir = fmt.Errorf("can not copy a directory")

// createTmpfs is a no-op on darwin
func createTmpfs(monRootfs string, path string, flags uint64, mode string, size string) error {
	return nil
}

// setupDev is a no-op on darwin
func setupDev(monRootfs string, devPath string) error {
	return nil
}

// fileFromHost is a no-op on darwin
func fileFromHost(monRootfs string, hostPath string, target string, mFlags int, withCopy bool) error {
	return nil
}

// bindMountFile is a no-op on darwin
func bindMountFile(hostPath string, dstDir string, dstPath string, perm uint32, mFlags int, isDir bool) error {
	return nil
}

// mapRootfsPropagationFlag is not applicable on darwin
func mapRootfsPropagationFlag(value string) (int, error) {
	return 0, fmt.Errorf("mount propagation not supported on darwin")
}

// rootfsParentMountPrivate is a no-op on darwin
func rootfsParentMountPrivate(path string) error {
	return nil
}

// prepareRoot is a no-op on darwin
func prepareRoot(path string, rootfsPropagation string) error {
	return nil
}

// mountVolumes is a no-op on darwin
func mountVolumes(rootfsPath string, mounts []specs.Mount) error {
	return nil
}

// mapMountFlag is not applicable on darwin
func mapMountFlag(value string) (mountFlagStruct, bool) {
	return mountFlagStruct{}, false
}

type mountFlagStruct struct {
	clear bool
	flag  int
}
