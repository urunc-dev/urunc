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
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
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
	defer func(g time.Duration) { guestShutdownGrace = g }(guestShutdownGrace)
	guestShutdownGrace = 200 * time.Millisecond
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
		{"an unconfirmed guest that stays up leaves it to the monitor", boot, unix.SIGTERM, ErrGuestSignalUnconfirmed, true, unix.SIGTERM},
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

// The guest init stops the agent before it syncs and powers off, so an
// unconfirmed signal must leave the monitor alone while it shuts down.
func TestSignalWithUnconfirmedGuestPoweringOff(t *testing.T) {
	defer func(g time.Duration) { guestShutdownGrace = g }(guestShutdownGrace)
	guestShutdownGrace = 2 * time.Second
	pid, died := monitorStandIn(t)
	u := unikontainerWithoutMonitor(t, pid)
	u.State.Annotations = map[string]string{
		annotHypervisor: "cloud-hypervisor",
		annotBootKernel: "/boot/bzImage",
		annotBootInitrd: "/boot/container-initrd",
	}
	u.UruncCfg = &UruncConfig{Monitors: map[string]types.MonitorConfig{
		"cloud-hypervisor": {BinaryPath: "/bin/true"},
	}}
	err := u.SignalWith(unix.SIGTERM, func(unix.Signal) error {
		// The guest powers off a moment after the session ends.
		time.AfterFunc(100*time.Millisecond, func() { _ = unix.Kill(pid, unix.SIGKILL) })
		return ErrGuestSignalUnconfirmed
	})
	require.NoError(t, err)
	sig, signaled := died()
	require.True(t, signaled)
	require.Equal(t, unix.SIGKILL, sig, "the monitor got urunc's signal, not the power off")
}

func TestMonitorGone(t *testing.T) {
	running := exec.Command("sleep", "30")
	require.NoError(t, running.Start())
	t.Cleanup(func() { _ = running.Process.Kill(); _ = running.Wait() })
	require.False(t, monitorGone(running.Process.Pid), "a running process")

	// Killed and not yet waited for, the process stays a zombie.
	zombie := exec.Command("sleep", "30")
	require.NoError(t, zombie.Start())
	require.NoError(t, zombie.Process.Kill())
	require.Eventually(t, func() bool { return monitorGone(zombie.Process.Pid) },
		2*time.Second, 10*time.Millisecond, "a zombie")
	require.NoError(t, unix.Kill(zombie.Process.Pid, 0), "the zombie is still there")
	_ = zombie.Wait()
	require.True(t, monitorGone(zombie.Process.Pid), "a reaped process")
}

const testAPISocket = "/run/api.sock"

// unikontainerWithCH returns a container run by Cloud Hypervisor, with its
// API socket configured and pid as the monitor's pid.
func unikontainerWithCH(t *testing.T, pid int) *Unikontainer {
	t.Helper()
	u := unikontainerWithoutMonitor(t, pid)
	u.State.Annotations[annotHypervisor] = string(hypervisors.CloudHypervisorVmm)
	u.UruncCfg.Monitors = map[string]types.MonitorConfig{
		string(hypervisors.CloudHypervisorVmm): {BinaryPath: "/nonexistent/cloud-hypervisor", SocketPath: testAPISocket},
	}
	return u
}

// serveAPI serves a fake monitor API on the unix socket sock, answering every
// request with status, and returns the number of requests it gets.
func serveAPI(t *testing.T, sock string, status int) *atomic.Int32 {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(sock), 0o755))
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	var calls atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
	}))
	require.NoError(t, srv.Listener.Close())
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return &calls
}

// startMonitor starts a process that stands in for the monitor.
func startMonitor(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

func TestSignalRequestsGuestShutdown(t *testing.T) {
	mon := startMonitor(t)
	u := unikontainerWithCH(t, mon.Process.Pid)
	sock := filepath.Join(u.monitorRootfs(), testAPISocket)
	calls := serveAPI(t, sock, http.StatusNoContent)

	require.NoError(t, u.Signal(unix.SIGTERM))
	require.EqualValues(t, 1, calls.Load())
	// The monitor and its socket stay, for a SIGKILL if the guest hangs.
	require.NoError(t, mon.Process.Signal(syscall.Signal(0)))
	require.FileExists(t, sock)
}

func TestSignalFallsBackToTheMonitor(t *testing.T) {
	mon := startMonitor(t)
	u := unikontainerWithCH(t, mon.Process.Pid)
	sock := filepath.Join(u.monitorRootfs(), testAPISocket)
	calls := serveAPI(t, sock, http.StatusInternalServerError)

	require.NoError(t, u.Signal(unix.SIGTERM))
	require.EqualValues(t, 1, calls.Load())
	var exitErr *exec.ExitError
	require.ErrorAs(t, mon.Wait(), &exitErr)
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok)
	require.Equal(t, syscall.SIGTERM, status.Signal())
	// The socket stays, for prepareControlSocket at the next start.
	require.FileExists(t, sock)
}

func TestSignalKillRemovesControlSocket(t *testing.T) {
	mon := startMonitor(t)
	u := unikontainerWithCH(t, mon.Process.Pid)
	sock := filepath.Join(u.monitorRootfs(), testAPISocket)
	calls := serveAPI(t, sock, http.StatusNoContent)

	require.NoError(t, u.Signal(unix.SIGKILL))
	require.Zero(t, calls.Load())
	require.NoFileExists(t, sock)
}

func TestOpenControlSocketStaysInMonitorRootfs(t *testing.T) {
	u := unikontainerWithCH(t, -1)
	sock := filepath.Join(u.monitorRootfs(), testAPISocket)
	serveAPI(t, sock, http.StatusNoContent)

	f, err := u.openControlSocket(testAPISocket)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// A symlink to a host socket resolves inside the monitor rootfs.
	hostSock := filepath.Join(t.TempDir(), "host.sock")
	serveAPI(t, hostSock, http.StatusNoContent)
	require.NoError(t, os.Remove(sock))
	require.NoError(t, os.Symlink(hostSock, sock))
	_, err = u.openControlSocket(testAPISocket)
	require.Error(t, err)

	require.NoError(t, os.Remove(sock))
	require.NoError(t, os.WriteFile(sock, nil, 0o600))
	_, err = u.openControlSocket(testAPISocket)
	require.ErrorContains(t, err, "not a socket")
}

func TestPrepareControlSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "run", "api.sock")

	// The directory is created.
	require.NoError(t, prepareControlSocket(sock))
	require.DirExists(t, filepath.Dir(sock))

	// A socket left by a monitor that exited on its own is removed.
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, l.Close())
	require.NoError(t, prepareControlSocket(sock))
	require.NoFileExists(t, sock)

	// Any other file stays, for the monitor's bind to fail on.
	require.NoError(t, os.WriteFile(sock, nil, 0o600))
	require.NoError(t, prepareControlSocket(sock))
	require.FileExists(t, sock)
}
