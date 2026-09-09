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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// chAPIClient drives a running Cloud Hypervisor process over its HTTP API on a
// unix socket. connect() opens one connection, and every request reuses it.
type chAPIClient struct {
	socketPath string
	httpClient *http.Client

	dialMu   sync.Mutex
	heldConn net.Conn
}

func newCHAPIClient(socketPath string) *chAPIClient {
	c := &chAPIClient{socketPath: socketPath}
	transport := &http.Transport{
		DialContext:         c.dialContext,
		IdleConnTimeout:     0,
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
	}
	c.httpClient = &http.Client{Transport: transport}
	return c
}

// dialContext hands over the connection connect() made, or dials again if it
// was already used.
func (c *chAPIClient) dialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	c.dialMu.Lock()
	defer c.dialMu.Unlock()
	if c.heldConn != nil {
		conn := c.heldConn
		c.heldConn = nil
		return conn, nil
	}
	var d net.Dialer
	return d.DialContext(ctx, "unix", c.socketPath)
}

// connect blocks until the API socket accepts connections, or the timeout
// elapses. It keeps the connection for the requests that follow.
func (c *chAPIClient) connect(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", c.socketPath, 50*time.Millisecond)
		if err == nil {
			c.dialMu.Lock()
			c.heldConn = conn
			c.dialMu.Unlock()
			return nil
		}
		lastErr = err
		time.Sleep(1 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = context.DeadlineExceeded
	}
	return fmt.Errorf("cloud-hypervisor API socket %q not ready within %s: %w", c.socketPath, timeout, lastErr)
}

func (c *chAPIClient) createVM(ctx context.Context, cfg *CHVMConfig) error {
	return c.put(ctx, "/api/v1/vm.create", cfg)
}

func (c *chAPIClient) bootVM(ctx context.Context) error {
	return c.put(ctx, "/api/v1/vm.boot", nil)
}

// put sends body as an HTTP PUT over the Unix socket and returns an error
// for any non-2xx response. A nil body sends an empty request.
func (c *chAPIClient) put(ctx context.Context, path string, body any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal %s request: %w", path, err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://localhost"+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build %s request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send %s request: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s returned HTTP %d: %s", path, resp.StatusCode, string(respBody))
	}
	return nil
}
