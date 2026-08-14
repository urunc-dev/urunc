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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
)

func TestGetRejectsContainerDirSymlink(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	bundleDir := t.TempDir()

	spec := &specs.Spec{
		Version: "1.0.2",
		Linux:   &specs.Linux{},
	}
	specData, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(bundleDir, configFilename), specData, 0o600))

	state := &specs.State{
		Version: "1.0.2",
		ID:      "victim",
		Bundle:  bundleDir,
		Annotations: map[string]string{
			annotType:       "linux",
			annotHypervisor: "qemu",
		},
	}
	stateData, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, stateFilename), stateData, 0o600))

	require.NoError(t, os.Symlink(outsideDir, filepath.Join(rootDir, state.ID)))

	_, err = Get(state.ID, rootDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}
