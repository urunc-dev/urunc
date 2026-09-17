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
