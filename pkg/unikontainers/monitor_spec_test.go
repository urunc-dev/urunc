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
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// newSpecUnikontainer builds the minimum Unikontainer that writeMonitorSpec
// needs: a spec, a state with the unikernel annotations, and the default urunc
// config. The returned RootfsParams points MonRootfs at monRootfs so the spec
// file lands in a real, writable directory.
func newSpecUnikontainer(t *testing.T, monRootfs string) (*Unikontainer, types.RootfsParams) {
	t.Helper()

	u := &Unikontainer{
		State: &specs.State{
			ID: "test-container",
			Annotations: map[string]string{
				annotType:       "unikraft",
				annotHypervisor: "qemu",
				annotVersion:    "1.0",
				annotBinary:     "/unikernel",
			},
		},
		Spec: &specs.Spec{
			Version: specs.Version,
			Root:    &specs.Root{Path: "rootfs", Readonly: true},
			Process: &specs.Process{
				Args: []string{"/unikernel", "--flag"},
				Env:  []string{"HOME=/root"},
				Cwd:  "/guest/workdir",
				User: specs.User{UID: 1000, GID: 1000},
			},
			Linux: &specs.Linux{
				Seccomp: &specs.LinuxSeccomp{DefaultAction: specs.ActErrno},
			},
			Annotations: map[string]string{},
		},
		UruncCfg: defaultUruncConfig(),
	}

	rootfsParams := types.RootfsParams{Type: "initrd", Path: "initrd.cpio", MonRootfs: monRootfs}

	return u, rootfsParams
}

// readMonitorSpecFile decodes the spec file written into dir directly, so the
// write-side test does not depend on LoadMonitorSpec (the read side).
func readMonitorSpecFile(t *testing.T, dir string) monitorSpec {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, monitorSpecFilename))
	require.NoError(t, err)

	var ms monitorSpec
	err = json.Unmarshal(data, &ms)
	require.NoError(t, err)

	return ms
}

func TestWriteMonitorSpec(t *testing.T) {
	t.Run("writes a spec that decodes back unchanged", func(t *testing.T) {
		t.Parallel()
		monRootfs := t.TempDir()
		u, rootfsParams := newSpecUnikontainer(t, monRootfs)

		err := u.writeMonitorSpec(rootfsParams, monitorResources{})
		require.NoError(t, err)

		got := readMonitorSpecFile(t, monRootfs)

		assert.Equal(t, "test-container", got.ContainerID)
		assert.Equal(t, "unikraft", got.UnikernelType)
		assert.Equal(t, "qemu", got.MonitorType)
		assert.Equal(t, u.UruncCfg.Monitors["qemu"], got.MonitorCfg)
		assert.Equal(t, specs.User{UID: 1000, GID: 1000}, got.User)
		// No knative annotation, so the network type is dynamic.
		assert.Equal(t, "dynamic", got.NetworkType)
		// The post-pivot process sees the monitor rootfs as "/".
		assert.Equal(t, "/", got.GuestParams.Rootfs.MonRootfs)
	})

	t.Run("does not persist the monitor environment", func(t *testing.T) {
		// t.Setenv forbids t.Parallel.
		t.Setenv("URUNC_TEST_SECRET", "do-not-write-me")
		monRootfs := t.TempDir()
		u, rootfsParams := newSpecUnikontainer(t, monRootfs)

		err := u.writeMonitorSpec(rootfsParams, monitorResources{})
		require.NoError(t, err)

		got := readMonitorSpecFile(t, monRootfs)
		assert.Nil(t, got.ExecArgs.Environment)

		// Not just absent from the decoded struct: absent from the file.
		data, err := os.ReadFile(filepath.Join(monRootfs, monitorSpecFilename))
		require.NoError(t, err)
		assert.NotContains(t, string(data), "do-not-write-me")
	})

	t.Run("keeps seccomp enabled when the spec confines it", func(t *testing.T) {
		t.Parallel()
		monRootfs := t.TempDir()
		u, rootfsParams := newSpecUnikontainer(t, monRootfs)

		err := u.writeMonitorSpec(rootfsParams, monitorResources{})
		require.NoError(t, err)

		got := readMonitorSpecFile(t, monRootfs)
		assert.True(t, got.ExecArgs.Seccomp)
	})

	t.Run("disables seccomp for an unconfined spec", func(t *testing.T) {
		t.Parallel()
		monRootfs := t.TempDir()
		u, rootfsParams := newSpecUnikontainer(t, monRootfs)
		u.Spec.Linux.Seccomp = nil

		err := u.writeMonitorSpec(rootfsParams, monitorResources{})
		require.NoError(t, err)

		got := readMonitorSpecFile(t, monRootfs)
		assert.False(t, got.ExecArgs.Seccomp)
	})

	t.Run("writes the file owner-only inside the monitor rootfs", func(t *testing.T) {
		t.Parallel()
		monRootfs := t.TempDir()
		u, rootfsParams := newSpecUnikontainer(t, monRootfs)

		err := u.writeMonitorSpec(rootfsParams, monitorResources{})
		require.NoError(t, err)

		info, err := os.Stat(filepath.Join(monRootfs, monitorSpecFilename))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})
}
