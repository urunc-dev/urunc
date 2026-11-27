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
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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
		require.NoError(t, u.Delete(false), "pid %d", pid)
		_, err := os.Stat(u.BaseDir)
		require.True(t, os.IsNotExist(err), "pid %d: %s still exists", pid, u.BaseDir)
	}
}

func TestDeleteWithoutSandboxNetNs(t *testing.T) {
	u := unikontainerWithoutMonitor(t, -1)
	// The sandbox is gone, and its network namespace with it.
	u.Spec.Linux.Namespaces = []specs.LinuxNamespace{
		{Type: specs.NetworkNamespace, Path: filepath.Join(t.TempDir(), "gone")},
	}
	require.NoError(t, u.Delete(false))
	_, err := os.Stat(u.BaseDir)
	require.True(t, os.IsNotExist(err), "%s still exists", u.BaseDir)
}

func TestDeleteKeepsStateWhenNetworkCleanupFails(t *testing.T) {
	u := unikontainerWithoutMonitor(t, -1)
	// A namespace path with a comma is refused, so the network cleanup fails.
	nsPath := filepath.Join(t.TempDir(), "net,ns")
	require.NoError(t, os.WriteFile(nsPath, nil, 0o600))
	u.Spec.Linux.Namespaces = []specs.LinuxNamespace{
		{Type: specs.NetworkNamespace, Path: nsPath},
	}

	require.Error(t, u.Delete(false))
	_, err := os.Stat(u.BaseDir)
	require.NoError(t, err, "a failed delete must keep the state, so it can be retried")

	require.NoError(t, u.Delete(true))
	_, err = os.Stat(u.BaseDir)
	require.True(t, os.IsNotExist(err), "%s still exists after a forced delete", u.BaseDir)
}

func TestCleanupFailedCreate(t *testing.T) {
	for _, test := range []string{"default PID file", "custom PID file"} {
		t.Run(test, func(t *testing.T) {
			u := unikontainerWithoutMonitor(t, 1234)
			u.State.Status = specs.StateCreated
			monitorRoot := filepath.Join(u.State.Bundle, monitorRootfsDirName)
			require.NoError(t, os.Mkdir(monitorRoot, 0o700))
			require.NoError(t, saveMonitorResources(u.BaseDir, monitorResources{}))

			pidPath := filepath.Join(u.State.Bundle, initPidFilename)
			pidArg := ""
			if test == "custom PID file" {
				pidPath = filepath.Join(t.TempDir(), "custom.pid")
				pidArg = pidPath
			}
			require.NoError(t, os.WriteFile(pidPath, []byte("1234"), 0o600))

			hookStatePath := filepath.Join(t.TempDir(), "poststop.json")
			u.Spec.Hooks = &specs.Hooks{Poststop: []specs.Hook{{
				Path: "/bin/sh",
				Args: []string{"sh", "-c", `cat > "$1"`, "poststop", hookStatePath},
			}}}
			require.NoError(t, u.CleanupFailedCreate(pidArg))

			for _, removed := range []string{pidPath, monitorRoot, u.BaseDir} {
				_, err := os.Stat(removed)
				require.ErrorIs(t, err, os.ErrNotExist)
			}
			require.DirExists(t, u.State.Bundle)
			hookState, err := os.ReadFile(hookStatePath)
			require.NoError(t, err)
			var stopped specs.State
			require.NoError(t, json.Unmarshal(hookState, &stopped))
			require.Equal(t, specs.StateStopped, stopped.Status)
			require.Zero(t, stopped.Pid)
		})
	}
}

func TestHookWithChildHoldingPipes(t *testing.T) {
	t.Parallel()
	timeout := 1
	for _, tc := range []struct {
		name         string
		command      string
		timeout      *int
		wantTimeout  bool
		wantExitCode int
	}{
		{
			name:        "timeout",
			command:     `/bin/cat "$1"; exit 0`,
			timeout:     &timeout,
			wantTimeout: true,
		},
		{
			name:    "successful exit with timeout",
			command: `/bin/cat "$1" & exit 0`,
			timeout: &timeout,
		},
		{
			name:    "successful exit without timeout",
			command: `/bin/cat "$1" & exit 0`,
		},
		{
			name:         "failed exit before timeout",
			command:      `/bin/sleep 0.2; /bin/cat "$1" & exit 17`,
			timeout:      &timeout,
			wantExitCode: 17,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fifo := filepath.Join(t.TempDir(), "release-child")
			require.NoError(t, unix.Mkfifo(fifo, 0o600))
			defer func() {
				fd, err := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
				if err == nil {
					_ = unix.Close(fd)
				}
			}()

			hook := specs.Hook{
				Path:    "/bin/sh",
				Args:    []string{"sh", "-c", tc.command, "hook", fifo},
				Timeout: tc.timeout,
			}
			done := make(chan error, 1)
			go func() { done <- executeHook(hook, nil) }()
			select {
			case err := <-done:
				switch {
				case tc.wantTimeout:
					require.ErrorIs(t, err, context.DeadlineExceeded)
				case tc.wantExitCode != 0:
					var exitErr *exec.ExitError
					require.ErrorAs(t, err, &exitErr)
					require.Equal(t, tc.wantExitCode, exitErr.ExitCode())
					require.NotErrorIs(t, err, context.DeadlineExceeded)
				default:
					require.NoError(t, err)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("hook did not return within four seconds")
			}
		})
	}
}
