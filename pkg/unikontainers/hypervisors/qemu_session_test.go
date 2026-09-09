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

package hypervisors

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeQMPServer speaks just enough of the QMP wire protocol for these tests:
// it sends the greeting on accept, answers qmp_capabilities and cont with
// {"return":{}}, and records every command in order. Before answering cont it
// emits an asynchronous RESUME event line, which clients must skip.
type fakeQMPServer struct {
	mu       sync.Mutex
	commands []string
	failCont bool
}

func (f *fakeQMPServer) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.commands))
	copy(out, f.commands)
	return out
}

func startFakeQMPServer(t *testing.T, failCont bool) (*fakeQMPServer, string) {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "qmp.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on %s: %v", sockPath, err)
	}
	t.Cleanup(func() { ln.Close() })

	srv := &fakeQMPServer{failCont: failCont}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte(`{"QMP":{"version":{},"capabilities":[]}}` + "\n"))
		rd := bufio.NewReader(conn)
		for {
			line, err := rd.ReadBytes('\n')
			if err != nil {
				return
			}
			var msg map[string]any
			if json.Unmarshal(line, &msg) != nil {
				return
			}
			cmd, _ := msg["execute"].(string)
			srv.mu.Lock()
			srv.commands = append(srv.commands, cmd)
			srv.mu.Unlock()
			if cmd == "cont" {
				_, _ = conn.Write([]byte(`{"event":"RESUME","timestamp":{"seconds":0,"microseconds":0}}` + "\n"))
				if srv.failCont {
					_, _ = conn.Write([]byte(`{"error":{"class":"GenericError","desc":"cont refused"}}` + "\n"))
					continue
				}
			}
			_, _ = conn.Write([]byte(`{"return":{}}` + "\n"))
		}
	}()
	return srv, sockPath
}

// TestQMPConnectNegotiatesAndResumes verifies the client reads the greeting,
// negotiates capabilities before anything else, and that Resume sends cont and
// tolerates the interleaved asynchronous event line.
func TestQMPConnectNegotiatesAndResumes(t *testing.T) {
	srv, sockPath := startFakeQMPServer(t, false)

	client, err := connectQMP(sockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("connectQMP failed: %v", err)
	}
	defer client.close()

	session := &QemuSession{client: client}
	if err := session.Resume(); err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	want := []string{"qmp_capabilities", "cont"}
	got := srv.recorded()
	if len(got) != len(want) {
		t.Fatalf("expected commands %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command %d: got %s, want %s", i, got[i], want[i])
		}
	}
}

// TestQMPResumeSurfacesErrors verifies a QMP error response becomes a Go error.
func TestQMPResumeSurfacesErrors(t *testing.T) {
	_, sockPath := startFakeQMPServer(t, true)

	client, err := connectQMP(sockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("connectQMP failed: %v", err)
	}
	defer client.close()

	session := &QemuSession{client: client}
	err = session.Resume()
	if err == nil {
		t.Fatal("Resume succeeded against a server that refused cont")
	}
}
