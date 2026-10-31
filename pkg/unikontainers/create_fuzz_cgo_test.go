//go:build cgo

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
	"reflect"
	"testing"

	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/specconv"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// containerConfig returns the monitor's libcontainer config, or nil if it cannot be built.
func containerConfig(u *Unikontainer) *configs.Config {
	cc, _ := u.BuildContainerConfig(false)
	return cc
}

// checkConfinement fails if the monitor's libcontainer config drops any of the
// container's namespaces, no-new-privileges, masked or read-only paths or OOM score.
func checkConfinement(t *testing.T, u *Unikontainer, got *configs.Config, bundle string) {
	if got == nil {
		return
	}
	spec := *u.Spec
	spec.Root = &specs.Root{Path: bundle}
	want, err := specconv.CreateLibcontainerConfig(&specconv.CreateOpts{CgroupName: "want", Spec: &spec})
	if err != nil {
		return
	}
	for _, field := range [][2]any{{got.Namespaces, want.Namespaces}, {got.NoNewPrivileges, want.NoNewPrivileges},
		{got.MaskPaths, want.MaskPaths}, {got.ReadonlyPaths, want.ReadonlyPaths},
		{got.OomScoreAdj, want.OomScoreAdj}} {
		if !reflect.DeepEqual(field[0], field[1]) {
			t.Fatalf("monitor confinement differs from the container's: %v, want %v", field[0], field[1])
		}
	}
}
