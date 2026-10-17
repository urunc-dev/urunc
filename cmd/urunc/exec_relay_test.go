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

package main

import (
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/urunc-dev/urunc/pkg/agentproto"
)

// fakeAgent answers frames the way urunit-agent does: an open request it
// cannot parse gets an Error frame on its stream, and the connection stays
// open for the session.
func fakeAgent(conn net.Conn) {
	defer conn.Close()
	for {
		f, err := agentproto.ReadFrame(conn)
		if err != nil {
			return
		}
		if f.Type != agentproto.TypeOpen {
			continue
		}
		var req agentproto.OpenRequest
		if err := json.Unmarshal(f.Payload, &req); err != nil {
			payload, _ := json.Marshal(agentproto.Error{Message: "bad open request: " + err.Error()})
			_ = agentproto.WriteFrame(conn, agentproto.TypeError, f.Stream, payload)
			continue
		}
		payload, _ := json.Marshal(agentproto.Exit{Code: 0})
		_ = agentproto.WriteFrame(conn, agentproto.TypeExit, f.Stream, payload)
	}
}

// The relay resets the first connections while the guest agent is not yet
// listening, which the host reads as EOF. dialAgentRelay has to retry those
// and return the first connection the agent answered on.
func TestDialAgentRelayRetriesUntilAgentAnswers(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "agent.vsock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	const refused = 3
	go func() {
		for i := 0; ; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if i < refused {
				_ = conn.Close()
				continue
			}
			go fakeAgent(conn)
		}
	}()

	conn, err := dialAgentRelay(sock, 10*time.Second)
	if err != nil {
		t.Fatalf("dialAgentRelay: %v", err)
	}
	defer conn.Close()

	// The session continues on the probed connection.
	req, _ := json.Marshal(agentproto.OpenRequest{Argv: []string{"true"}})
	if err := agentproto.WriteFrame(conn, agentproto.TypeOpen, 7, req); err != nil {
		t.Fatal(err)
	}
	f, err := agentproto.ReadFrame(conn)
	if err != nil {
		t.Fatalf("reading the session's reply: %v", err)
	}
	if f.Type != agentproto.TypeExit || f.Stream != 7 {
		t.Fatalf("got frame type %d on stream %d, want Exit on stream 7", f.Type, f.Stream)
	}
}

// A relay that never gets an agent fails once the timeout elapses.
func TestDialAgentRelayTimesOut(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "agent.vsock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	if _, err := dialAgentRelay(sock, 500*time.Millisecond); err == nil {
		t.Fatal("expected an error with no agent behind the relay")
	}
}

// The probe accepts only the agent's Error frame on the probe stream.
func TestRelayProbeRejectsOtherFrames(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		_, _ = agentproto.ReadFrame(server)
		_ = agentproto.WriteFrame(server, agentproto.TypeStdout, relayProbeStream, []byte("x"))
		_, _ = io.Copy(io.Discard, server)
	}()

	if err := relayProbe(client); err == nil {
		t.Fatal("expected an error for a non-Error answer")
	}
}
