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
	"github.com/stretchr/testify/assert"
)

func writeBundleConfig(t *testing.T, bundleDir string, configJSON string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(bundleDir, configFilename), []byte(configJSON), 0o600)
	assert.NoError(t, err)
}

func TestNewRejectsMissingProcess(t *testing.T) {
	cfg := UruncConfigFromMap(map[string]string{})

	t.Run("linux and root without process returns process validation error", func(t *testing.T) {
		t.Parallel()
		bundleDir := t.TempDir()
		writeBundleConfig(t, bundleDir, `{
			"linux": {},
			"root": {"path": "rootfs"},
			"annotations": {
				"com.urunc.unikernel.unikernelType": "unikraft",
				"com.urunc.unikernel.hypervisor": "qemu",
				"com.urunc.unikernel.binary": "/kernel"
			}
		}`)

		u, err := New(bundleDir, "test-container", t.TempDir(), cfg)
		assert.Nil(t, u)
		assert.Error(t, err)
		assert.EqualError(t, err, "invalid OCI spec: process section is required")
	})

	t.Run("missing linux still returns linux validation error", func(t *testing.T) {
		t.Parallel()
		bundleDir := t.TempDir()
		writeBundleConfig(t, bundleDir, `{"root":{"path":"rootfs"},"process":{}}`)

		u, err := New(bundleDir, "test-container", t.TempDir(), cfg)
		assert.Nil(t, u)
		assert.Error(t, err)
		assert.EqualError(t, err, "invalid OCI spec: linux section is required")
	})
}

func TestGetRejectsMissingProcess(t *testing.T) {
	t.Parallel()

	bundleDir := t.TempDir()
	writeBundleConfig(t, bundleDir, `{
		"linux": {},
		"root": {"path": "rootfs"}
	}`)

	rootDir := t.TempDir()
	containerID := "test-container"
	containerDir := filepath.Join(rootDir, containerID)
	err := os.MkdirAll(containerDir, 0o755)
	assert.NoError(t, err)

	state := specs.State{
		Version: "1.0.0",
		ID:      containerID,
		Status:  "created",
		Bundle:  bundleDir,
		Annotations: map[string]string{
			annotType: "unikraft",
		},
	}
	stateData, err := json.Marshal(state)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(containerDir, stateFilename), stateData, 0o600)
	assert.NoError(t, err)

	u, err := Get(containerID, rootDir)
	assert.Nil(t, u)
	assert.Error(t, err)
	assert.EqualError(t, err, "invalid OCI spec: process section is required")
}
