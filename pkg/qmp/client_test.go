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

package qmp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testGreeting = `{"QMP": {"version": {"qemu": {"major": 9, "minor": 1, "micro": 2}, "package": ""}, "capabilities": ["oob"]}}`

// fakeQEMU serves one QMP connection on a unix socket: it sends the greeting,
// then answers each request through reply and records the commands it saw.
type fakeQEMU struct {
	sock     string
	ln       net.Listener
	commands chan string
	done     chan error
}

func newFakeQEMU(t *testing.T, reply func(req QMPRequest) []string) *fakeQEMU {
	t.Helper()

	// Keep the path short: macOS limits unix socket paths to 104 bytes.
	dir, err := os.MkdirTemp("", "qmp")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "q.sock")

	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	f := &fakeQEMU{sock: sock, ln: ln, commands: make(chan string, 16), done: make(chan error, 1)}
	go func() {
		f.done <- f.serve(reply)
	}()
	return f
}

func (f *fakeQEMU) serve(reply func(req QMPRequest) []string) error {
	conn, err := f.ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := fmt.Fprintln(conn, testGreeting); err != nil {
		return err
	}
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			// The client closed the connection.
			return nil
		}
		var req QMPRequest
		if err := json.Unmarshal(line, &req); err != nil {
			return fmt.Errorf("bad request %q: %w", line, err)
		}
		f.commands <- req.Execute
		for _, out := range reply(req) {
			if _, err := fmt.Fprintln(conn, out); err != nil {
				return err
			}
		}
	}
}

// okReply answers every request with an empty return carrying its id.
func okReply(req QMPRequest) []string {
	return []string{fmt.Sprintf(`{"return": {}, "id": %d}`, req.ID)}
}

func TestDialNegotiatesCapabilitiesAndExecutes(t *testing.T) {
	f := newFakeQEMU(t, func(req QMPRequest) []string {
		if req.Execute == "query-status" {
			// An event may arrive before the reply; the client skips it.
			return []string{
				`{"event": "RESUME", "data": {}}`,
				fmt.Sprintf(`{"return": {"status": "running", "running": true}, "id": %d}`, req.ID),
			}
		}
		return okReply(req)
	})

	c, err := Dial(f.sock, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "qmp_capabilities", <-f.commands, "Dial must send qmp_capabilities first")

	status, err := c.QueryStatus()
	require.NoError(t, err)
	assert.Equal(t, "query-status", <-f.commands)
	assert.Equal(t, map[string]interface{}{"status": "running", "running": true}, status)

	require.NoError(t, c.SystemPowerdown())
	assert.Equal(t, "system_powerdown", <-f.commands)

	require.NoError(t, c.Close())
	assert.NoError(t, <-f.done)
}

func TestExecuteReportsQMPError(t *testing.T) {
	f := newFakeQEMU(t, func(req QMPRequest) []string {
		if req.Execute == "system_powerdown" {
			return []string{fmt.Sprintf(`{"error": {"class": "GenericError", "desc": "no"}, "id": %d}`, req.ID)}
		}
		return okReply(req)
	})

	c, err := Dial(f.sock, time.Second)
	require.NoError(t, err)
	defer c.Close()

	err = c.SystemPowerdown()
	assert.ErrorContains(t, err, "system_powerdown failed")
}

func TestDialFailsWhenCapabilitiesAreRefused(t *testing.T) {
	f := newFakeQEMU(t, func(req QMPRequest) []string {
		return []string{fmt.Sprintf(`{"error": {"class": "CommandNotFound", "desc": "no"}, "id": %d}`, req.ID)}
	})

	_, err := Dial(f.sock, time.Second)
	assert.ErrorContains(t, err, "failed to enable QMP capabilities")
}

func TestDialFailsWithoutSocket(t *testing.T) {
	_, err := Dial(filepath.Join(t.TempDir(), "missing.sock"), 100*time.Millisecond)
	assert.ErrorContains(t, err, "failed to connect to QMP socket")
}
