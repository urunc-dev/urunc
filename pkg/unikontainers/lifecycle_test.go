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
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// A container whose monitor never started keeps the initial Pid -1 in its
// state, and a state saved without a pid reads back as 0. kill(2) treats
// both as process groups, so none of these may reach a signal.
var unknownMonitorPids = []int{-1, 0}

func unikontainerWithoutMonitor(t *testing.T, pid int) *Unikontainer {
	t.Helper()
	return &Unikontainer{
		State: &specs.State{
			ID:          "no-monitor",
			Bundle:      t.TempDir(),
			Pid:         pid,
			Annotations: map[string]string{annotHypervisor: "qemu"},
		},
		Spec:     &specs.Spec{Root: &specs.Root{Path: "rootfs"}, Linux: &specs.Linux{}},
		BaseDir:  t.TempDir(),
		UruncCfg: &UruncConfig{},
	}
}

func TestSignalWithoutMonitorPid(t *testing.T) {
	for _, pid := range unknownMonitorPids {
		u := unikontainerWithoutMonitor(t, pid)
		require.NoError(t, u.Signal(unix.SIGKILL), "pid %d", pid)
		require.NoError(t, u.Signal(unix.SIGTERM), "pid %d", pid)
	}
}

func TestKillWithoutMonitorPid(t *testing.T) {
	for _, pid := range unknownMonitorPids {
		u := unikontainerWithoutMonitor(t, pid)
		// The CRI shape: the container joins the pod's namespace by path.
		u.Spec.Linux.Namespaces = []specs.LinuxNamespace{
			{Type: specs.NetworkNamespace, Path: "/proc/self/ns/net"},
		}
		require.NoError(t, u.Kill(), "pid %d", pid)
	}
}

func TestIsRunningWithoutMonitorPid(t *testing.T) {
	for _, pid := range unknownMonitorPids {
		u := unikontainerWithoutMonitor(t, pid)
		require.False(t, u.isRunning(), "pid %d", pid)
	}
}

func TestDeleteWithoutMonitorPid(t *testing.T) {
	for _, pid := range unknownMonitorPids {
		u := unikontainerWithoutMonitor(t, pid)
		require.NoError(t, os.WriteFile(filepath.Join(u.BaseDir, stateFilename), []byte("{}"), 0o600))
		require.NoError(t, u.Delete(), "pid %d", pid)
		_, err := os.Stat(u.BaseDir)
		require.True(t, os.IsNotExist(err), "pid %d: %s still exists", pid, u.BaseDir)
	}
}
