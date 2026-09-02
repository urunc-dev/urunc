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
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cavaliergopher/cpio"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// TODO: Move this alongside the tests for newRootfsBuilder in unikontainers.go.
func TestNewRootfsBuilderPassesGuestTypeToInitrd(t *testing.T) {
	u := &Unikontainer{
		State: &specs.State{Annotations: map[string]string{annotType: unikernels.UnikraftUnikernel}},
		Spec:  &specs.Spec{},
	}

	builder := u.newRootfsBuilder(types.RootfsParams{Type: "initrd"}, nil, "", "", 0)
	rfs, ok := builder.(initrdRootfs)
	require.True(t, ok)
	require.Equal(t, unikernels.UnikraftUnikernel, rfs.guestType)
}

func TestInitrdRootfsPostSetupSelectsUpdateByGuestType(t *testing.T) {
	tests := []struct {
		name         string
		guestType    string
		trailerCount int
	}{
		{name: "Unikraft merges", guestType: unikernels.UnikraftUnikernel, trailerCount: 1},
		{name: "Linux appends", guestType: unikernels.LinuxUnikernel, trailerCount: 2},
		{name: "unknown appends", guestType: "other", trailerCount: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			archivePath := filepath.Join(dir, "initrd.cpio")
			archive, err := os.Create(archivePath)
			require.NoError(t, err)
			require.NoError(t, cpio.NewWriter(archive).Close())
			require.NoError(t, archive.Close())

			source := filepath.Join(dir, "mounted")
			require.NoError(t, os.WriteFile(source, []byte("new"), 0o600))
			rfs := initrdRootfs{
				initrdHostFullPath: archivePath,
				guestType:          tt.guestType,
				mounts: []specs.Mount{
					{Type: "bind", Source: source, Destination: "/mounted"},
				},
			}

			require.NoError(t, rfs.postSetup())
			updated, err := os.ReadFile(archivePath)
			require.NoError(t, err)
			require.Equal(t, tt.trailerCount, bytes.Count(updated, []byte("TRAILER!!!\x00")))
		})
	}
}
