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

package initrd

import "errors"

// Initrd is a stub for darwin
type Initrd struct{}

// NewInitrd creates a new Initrd (stub on darwin)
func NewInitrd(path string) (*Initrd, error) {
	return &Initrd{}, nil
}

// AddFileToInitrd is unsupported on darwin: the darwin runner delivers the
// urunit config through the 9pfs/virtiofs rootfs (createFile), not by
// rewriting a cpio initrd. Present so the shared Linux unikernel builder
// compiles; only reached for initrd-rootfs images, which darwin does not use.
func AddFileToInitrd(_ string, _ string, _ string) error {
	return errors.New("initrd rewriting is not supported on darwin (use a 9pfs/virtiofs rootfs)")
}
