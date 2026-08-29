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
	"fmt"
	"net"
	"time"
)

// qmpClient drives a running QEMU process over its QMP socket, which carries
// line-delimited JSON. It implements only what urunc needs.
type qmpClient struct {
	conn net.Conn
	rd   *bufio.Reader
}

// connectQMP blocks until the QMP socket accepts a connection, then runs the
// handshake. QMP requires it before any command.
func connectQMP(socketPath string, timeout time.Duration) (*qmpClient, error) {
	deadline := time.Now().Add(timeout)
	var conn net.Conn
	var lastErr error
	for time.Now().Before(deadline) {
		conn, lastErr = net.DialTimeout("unix", socketPath, 50*time.Millisecond)
		if lastErr == nil {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}
	if conn == nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("dial timed out after %s", timeout)
		}
		return nil, fmt.Errorf("qmp socket %q not ready within %s: %w", socketPath, timeout, lastErr)
	}
	c := &qmpClient{conn: conn, rd: bufio.NewReader(conn)}
	// Bound the handshake itself: a dial succeeding does not guarantee QEMU
	// keeps talking, so reads/writes below must not block forever.
	_ = c.conn.SetDeadline(deadline)
	// The server speaks first: nothing may be sent before its greeting is read.
	greeting, err := c.readMessage()
	if err != nil {
		c.close()
		return nil, fmt.Errorf("failed to read the QMP greeting: %w", err)
	}
	if _, ok := greeting["QMP"]; !ok {
		c.close()
		return nil, fmt.Errorf("unexpected QMP greeting: %v", greeting)
	}
	if err := c.execute("qmp_capabilities"); err != nil {
		c.close()
		return nil, err
	}
	_ = c.conn.SetDeadline(time.Time{})
	return c, nil
}

func (c *qmpClient) execute(command string) error {
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()

	req, err := json.Marshal(map[string]string{"execute": command})
	if err != nil {
		return fmt.Errorf("failed to marshal QMP %s: %w", command, err)
	}
	if _, err := c.conn.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("failed to send QMP %s: %w", command, err)
	}
	resp, err := c.readMessage()
	if err != nil {
		return fmt.Errorf("failed to read the QMP response to %s: %w", command, err)
	}
	if errObj, ok := resp["error"]; ok {
		return fmt.Errorf("QMP %s failed: %v", command, errObj)
	}
	if _, ok := resp["return"]; !ok {
		return fmt.Errorf("unexpected QMP response to %s: %v", command, resp)
	}
	return nil
}

// readMessage returns the next QMP message that is not an asynchronous event.
// QEMU may interleave event lines (e.g. RESUME) with command responses.
func (c *qmpClient) readMessage() (map[string]any, error) {
	for {
		line, err := c.rd.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var msg map[string]any
		if err := json.Unmarshal(line, &msg); err != nil {
			return nil, fmt.Errorf("invalid QMP message %q: %w", line, err)
		}
		if _, isEvent := msg["event"]; isEvent {
			continue
		}
		return msg, nil
	}
}

func (c *qmpClient) close() {
	_ = c.conn.Close()
}
