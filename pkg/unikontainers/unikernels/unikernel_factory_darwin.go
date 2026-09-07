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

package unikernels

import (
	"errors"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

var ErrNotSupportedUnikernel = errors.New("unikernel is not supported")

func New(unikernelType string) (types.Unikernel, error) {
	switch unikernelType {
	case RumprunUnikernel:
		unikernel := newRumprun()
		return unikernel, nil
	case MirageUnikernel:
		unikernel := newMirage()
		return unikernel, nil
	case MewzUnikernel:
		unikernel := newMewz()
		return unikernel, nil
	case "linux":
		// The real, shared Linux unikernel builder — the same one the Linux
		// engine uses — so darwin produces an identical kernel command line
		// (root=/console=/ip=/urunit config) for the same image.
		unikernel := newLinux()
		return unikernel, nil
	case "unikraft":
		// Unikraft is Linux-only
		return nil, errors.New("unikraft is not supported on darwin")
	default:
		return nil, ErrNotSupportedUnikernel
	}
}
