//go:build linux

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
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
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

// monitorStandIn starts a process that stands in for the monitor and returns
// its pid and a function that reports, without blocking for long, whether it
// was killed by a signal.
func monitorStandIn(t *testing.T) (int, func() (unix.Signal, bool)) {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())
	exited := make(chan *os.ProcessState, 1)
	go func() {
		_ = cmd.Wait()
		exited <- cmd.ProcessState
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd.Process.Pid, func() (unix.Signal, bool) {
		select {
		case st := <-exited:
			ws := st.Sys().(syscall.WaitStatus)
			return unix.Signal(ws.Signal()), ws.Signaled()
		case <-time.After(500 * time.Millisecond):
			return 0, false
		}
	}
}

// A generic container boot hands a stop signal to the guest, and the monitor
// gets it only for SIGKILL, for another container, or when the guest fails.
func TestSignalWithGuest(t *testing.T) {
	boot := map[string]string{
		annotHypervisor: "cloud-hypervisor",
		annotBootKernel: "/boot/bzImage",
		annotBootInitrd: "/boot/container-initrd",
	}
	cases := []struct {
		name     string
		annot    map[string]string
		sig      unix.Signal
		guestErr error
		guest    bool        // the guest is asked
		monitor  unix.Signal // what the monitor dies of; 0 for nothing
	}{
		{"SIGTERM goes to the guest", boot, unix.SIGTERM, nil, true, 0},
		{"SIGINT goes to the guest", boot, unix.SIGINT, nil, true, 0},
		{"a failed guest leaves it to the monitor", boot, unix.SIGTERM, errors.New("no agent"), true, unix.SIGTERM},
		{"SIGKILL goes to the monitor", boot, unix.SIGKILL, nil, false, unix.SIGKILL},
		{"another container signals the monitor", map[string]string{annotHypervisor: "cloud-hypervisor"}, unix.SIGTERM, nil, false, unix.SIGTERM},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pid, died := monitorStandIn(t)
			u := unikontainerWithoutMonitor(t, pid)
			u.State.Annotations = tc.annot
			u.UruncCfg = &UruncConfig{Monitors: map[string]types.MonitorConfig{
				"cloud-hypervisor": {BinaryPath: "/bin/true"},
			}}
			var asked []unix.Signal
			err := u.SignalWith(tc.sig, func(s unix.Signal) error {
				asked = append(asked, s)
				return tc.guestErr
			})
			require.NoError(t, err)
			if tc.guest {
				require.Equal(t, []unix.Signal{tc.sig}, asked)
			} else {
				require.Empty(t, asked)
			}
			sig, signaled := died()
			if tc.monitor == 0 {
				require.False(t, signaled, "the monitor was signalled")
			} else {
				require.True(t, signaled, "the monitor was not signalled")
				require.Equal(t, tc.monitor, sig)
			}
		})
	}
}
