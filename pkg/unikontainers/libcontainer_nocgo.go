//go:build !cgo

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

// The libcontainer monitor path requires cgo (runc's specconv and libcontainer
// packages). In a non-cgo build -- the containerd shim -- that path is never
// taken, so this stub stands in for destroyLibcontainer, which Delete references
// unconditionally. BuildContainerConfig and LibcontainerRoot need no stub:
// nothing compiled in a non-cgo build references them.

package unikontainers

// destroyLibcontainer is a no-op in a non-cgo build; see libcontainer_cgo.go for
// the real implementation.
func (u *Unikontainer) destroyLibcontainer() error {
	return nil
}
