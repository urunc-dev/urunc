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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urunc-dev/urunc/pkg/agentproto"
)

// dialAgent stands up the real serveConn over a unix socket (the transport
// the protocol doc calls out for tests) and returns a connected client.
func dialAgent(t *testing.T) net.Conn {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		serveConn(conn) // the real in-guest agent connection handler
	}()
	cli, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

// runSession opens one exec session on the given stream and drains frames
// until the agent reports Exit, returning the collected stdout/stderr/code.
// Frames of other streams are ignored.
func runSession(t *testing.T, cli net.Conn, stream uint32, req agentproto.OpenRequest) (stdout, stderr string, code int) {
	t.Helper()
	if err := agentproto.WriteJSON(cli, agentproto.TypeOpen, stream, req); err != nil {
		t.Fatalf("send open: %v", err)
	}
	var out, errb bytes.Buffer
	_ = cli.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		f, err := agentproto.ReadFrame(cli)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if f.Stream != stream {
			continue
		}
		switch f.Type {
		case agentproto.TypeStdout:
			out.Write(f.Payload)
		case agentproto.TypeStderr:
			errb.Write(f.Payload)
		case agentproto.TypeError:
			var e agentproto.Error
			_ = json.Unmarshal(f.Payload, &e)
			t.Fatalf("agent error on stream %d: %s", f.Stream, e.Message)
		case agentproto.TypeExit:
			var ex agentproto.Exit
			if err := json.Unmarshal(f.Payload, &ex); err != nil {
				t.Fatalf("bad exit payload: %v", err)
			}
			return out.String(), errb.String(), ex.Code
		}
	}
}

// TestAgentExecNonTTY drives a real exec session end to end: the agent
// spawns /bin/sh, streams stdout and stderr back as separate frame types,
// and reports the child's exit code.
func TestAgentExecNonTTY(t *testing.T) {
	cli := dialAgent(t)
	stdout, stderr, code := runSession(t, cli, 1, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", "echo out-msg; echo err-msg 1>&2; exit 7"},
	})
	if !strings.Contains(stdout, "out-msg") {
		t.Errorf("stdout missing marker, got %q", stdout)
	}
	if !strings.Contains(stderr, "err-msg") {
		t.Errorf("stderr missing marker, got %q", stderr)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

// TestAgentExecTTY exercises the pty path: stdin/stdout/stderr are one
// terminal, so output arrives as TypeStdout and the child sees a tty.
func TestAgentExecTTY(t *testing.T) {
	cli := dialAgent(t)
	stdout, _, code := runSession(t, cli, 2, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", "tty >/dev/null && echo is-a-tty"},
		TTY:  true,
		Rows: 24, Cols: 80,
	})
	if !strings.Contains(stdout, "is-a-tty") {
		t.Errorf("tty session output missing marker, got %q", stdout)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

// TestAgentExecOutputBeforeExit runs short sessions that write their output
// just before they exit, on both the pipe and the pty path. The client ends a
// session on its Exit frame, so the output has to reach it first. The race is
// timing dependent, hence the loop.
func TestAgentExecOutputBeforeExit(t *testing.T) {
	cli := dialAgent(t)
	for i := uint32(0); i < 200; i++ {
		for _, tty := range []bool{false, true} {
			stream := 100 + 2*i
			if tty {
				stream++
			}
			marker := fmt.Sprintf("marker-%d", stream)
			stdout, _, code := runSession(t, cli, stream, agentproto.OpenRequest{
				Argv: []string{"/bin/echo", marker},
				TTY:  tty,
			})
			if !strings.Contains(stdout, marker) {
				t.Fatalf("session %d (tty=%v) lost its output: got %q, want %q", stream, tty, stdout, marker)
			}
			if code != 0 {
				t.Fatalf("session %d (tty=%v) exit code = %d, want 0", stream, tty, code)
			}
		}
	}
}

// TestAgentExecBackgroundHoldsOutput leaves a process in the background that
// keeps the session's stdout open for 10 seconds. The exit report still
// arrives, with the output written before it, long before that process ends.
func TestAgentExecBackgroundHoldsOutput(t *testing.T) {
	cli := dialAgent(t)
	start := time.Now()
	stdout, _, code := runSession(t, cli, 4, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", "echo early; sleep 10 &"},
	})
	elapsed := time.Since(start)
	if !strings.Contains(stdout, "early") {
		t.Errorf("stdout missing marker, got %q", stdout)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if elapsed > 5*time.Second {
		t.Errorf("exit reported after %v, want it within 5s", elapsed)
	}
}

// TestAgentStdinRoundTrip feeds bytes to the child over TypeStdin and reads
// them back, checking the full duplex stdin path and CloseStdin handling.
func TestAgentStdinRoundTrip(t *testing.T) {
	cli := dialAgent(t)
	if err := agentproto.WriteJSON(cli, agentproto.TypeOpen, 3, agentproto.OpenRequest{
		Argv: []string{"/bin/cat"},
	}); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := agentproto.WriteFrame(cli, agentproto.TypeStdin, 3, []byte("ping-pong\n")); err != nil {
		t.Fatalf("stdin: %v", err)
	}
	if err := agentproto.WriteFrame(cli, agentproto.TypeCloseStdin, 3, nil); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	var out bytes.Buffer
	_ = cli.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		f, err := agentproto.ReadFrame(cli)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if f.Type == agentproto.TypeStdout {
			out.Write(f.Payload)
		}
		if f.Type == agentproto.TypeExit {
			break
		}
	}
	if !strings.Contains(out.String(), "ping-pong") {
		t.Errorf("cat did not echo stdin, got %q", out.String())
	}
}
