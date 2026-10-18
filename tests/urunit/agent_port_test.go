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

//go:build linux

package urunit

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/urunc-dev/urunc/pkg/agentproto"
	"golang.org/x/sys/unix"
)

// waitFor polls cond every 20 ms until it holds or timeout passes.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestAgentPortPeerLossKillsSessions serves an inherited socket with the
// semantics of the virtio port (URUNIT_AGENT_TRANSPORT=fd:3). When the peer
// goes away, its sessions die, but the agent lives on and keeps the port
// open for the next peer.
func TestAgentPortPeerLossKillsSessions(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	agentEnd := os.NewFile(uintptr(fds[0]), "agent-port")
	testEnd := os.NewFile(uintptr(fds[1]), "test-port")
	defer agentEnd.Close()
	fc, err := net.FileConn(testEnd)
	_ = testEnd.Close()
	if err != nil {
		t.Fatalf("file conn: %v", err)
	}
	conn := fc.(*net.UnixConn)
	defer conn.Close()

	agent := startAgent(t, "fd:3", nil, nil, agentEnd)

	pidFile := filepath.Join(t.TempDir(), "pid")
	if err := agentproto.WriteJSON(conn, agentproto.TypeOpen, 1, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", `echo $$ > "$1"; exec sleep 100`, "sh", pidFile},
	}); err != nil {
		t.Fatalf("open: %v", err)
	}
	var pid int
	if !waitFor(testDeadline, func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil || !strings.HasSuffix(string(b), "\n") {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil
	}) {
		t.Fatalf("the session did not write its pid")
	}
	// The pid file is written before the exec. Wait for the exec, so that
	// the session is the sleep that the agent has to kill.
	if !waitFor(5*time.Second, func() bool {
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		return err == nil && strings.TrimSpace(string(comm)) == "sleep"
	}) {
		t.Fatalf("pid %d did not exec sleep", pid)
	}

	if err := conn.CloseWrite(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// (a) The session dies, and the agent reaps it, within 2 s.
	if !waitFor(2*time.Second, func() bool {
		return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
	}) {
		t.Errorf("session pid %d still exists 2s after the peer left", pid)
	}

	// (b) The agent lives on.
	time.Sleep(500 * time.Millisecond)
	if err := agent.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("agent is gone after the peer left: %v", err)
	}
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", agent.Process.Pid)); err == nil {
		// The third field is the state. A zombie has exited.
		if f := strings.Fields(string(b)); len(f) > 2 && f[2] == "Z" {
			t.Fatalf("agent exited after the peer left")
		}
	}

	// (c) The agent still holds the port as fd 3.
	got, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/3", agent.Process.Pid))
	if err != nil {
		t.Fatalf("agent closed fd 3: %v", err)
	}
	want, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", agentEnd.Fd()))
	if err != nil {
		t.Fatalf("readlink own end: %v", err)
	}
	if got != want {
		t.Errorf("agent fd 3 -> %s, want the port %s", got, want)
	}

	// The peer's half-closed socket still delivers anything the agent
	// wrote. It must not be an Exit frame of the killed session.
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	for {
		f, err := agentproto.ReadFrame(conn)
		if err != nil {
			break
		}
		if f.Type == agentproto.TypeExit || f.Type == agentproto.TypeError {
			t.Errorf("frame type %d on stream %d after the peer left: %q", f.Type, f.Stream, f.Payload)
		}
	}
}
