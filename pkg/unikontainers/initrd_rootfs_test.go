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
	"strings"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestInitrdRootfs_PreSetup(t *testing.T) {
	i := initrdRootfs{}

	if err := i.preSetup(); err != nil {
		t.Errorf("preSetup() returned unexpected error: %v", err)
	}
}

func TestInitrdRootfs_PreStart(t *testing.T) {
	i := initrdRootfs{}

	if err := i.preStart(); err != nil {
		t.Errorf("preStart() returned unexpected error: %v", err)
	}
}

func TestInitrdRootfs_GetBlockDevs(t *testing.T) {
	i := initrdRootfs{}

	devs, err := i.getBlockDevs()
	if err != nil {
		t.Fatalf("getBlockDevs() returned unexpected error: %v", err)
	}
	if devs != nil {
		t.Errorf("getBlockDevs() = %v, want nil", devs)
	}
}

func TestInitrdRootfs_GetSharedDirs(t *testing.T) {
	i := initrdRootfs{}

	dirs, err := i.getSharedDirs()
	if err != nil {
		t.Fatalf("getSharedDirs() returned unexpected error: %v", err)
	}

	want := types.SharedfsParams{}
	if !reflect.DeepEqual(dirs, want) {
		t.Errorf("getSharedDirs() = %+v, want empty %+v", dirs, want)
	}
}

func TestInitrdRootfs_GetMounts(t *testing.T) {
	i := initrdRootfs{}

	mounts, err := i.getMounts()
	if err != nil {
		t.Fatalf("getMounts() returned unexpected error: %v", err)
	}

	if len(mounts) != 1 {
		t.Fatalf("getMounts() returned %d mounts, want 1", len(mounts))
	}

	got := mounts[0]
	if got.Destination != "/tmp" {
		t.Errorf("getMounts()[0].Destination = %q, want %q", got.Destination, "/tmp")
	}
}

func TestInitrdRootfs_PostSetup_InvalidPath(t *testing.T) {
	i := initrdRootfs{
		initrdHostFullPath: "/definitely/does/not/exist/initrd",
		mounts: []specs.Mount{
			{
				Destination: "/data",
				Source:      "/host/data",
				Type:        "bind",
				Options:     []string{"bind", "ro"},
			},
		},
	}

	err := i.postSetup()
	if err == nil {
		t.Fatal("postSetup() expected an error for a non-existent initrd path, got nil")
	}

	const wantPrefix = "failed to update guest's initrd"
	if !strings.Contains(err.Error(), wantPrefix) {
		t.Errorf("postSetup() error = %q, want it to contain %q", err.Error(), wantPrefix)
	}
}

func TestInitrdRootfs_PostSetup_NoMounts_InvalidPath(t *testing.T) {
	i := initrdRootfs{
		initrdHostFullPath: "/definitely/does/not/exist/initrd",
		mounts:             nil,
	}

	// Even with no mounts to inject, an invalid initrd path should
	// still surface as an error (or at minimum should not panic).
	err := i.postSetup()
	if err != nil && !strings.Contains(err.Error(), "failed to update guest's initrd") {
		t.Errorf("postSetup() error = %q, want it wrapped with %q", err.Error(), "failed to update guest's initrd")
	}
}
