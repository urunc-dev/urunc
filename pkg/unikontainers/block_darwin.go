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
	"errors"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// getMountInfo is unsupported on darwin: there is no host mount namespace to
// inspect, so the container rootfs cannot be presented to the guest as a block
// device. Returning an error makes the shared ChooseRootfs selector skip the
// block path and fall through to a shared-fs (9pfs/virtiofs), which is how the
// darwin monitors mount the rootfs. Present so the platform-neutral rootfs
// selector links on darwin.
func getMountInfo(_ string) (types.BlockDevParams, error) {
	return types.BlockDevParams{}, errors.New("block-device rootfs is not supported on darwin (use a 9pfs/virtiofs rootfs)")
}
