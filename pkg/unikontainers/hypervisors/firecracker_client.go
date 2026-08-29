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

// firecrackerClient drives a running Firecracker process over its HTTP API on
// a unix socket. connect() opens one connection, and every request reuses it.
type firecrackerClient struct {
	socketPath string
	httpClient *http.Client

	dialMu   sync.Mutex
	heldConn net.Conn
}

// newFirecrackerClient returns a client for the API socket at socketPath.
// Requests are addressed to "http://localhost" but travel over that socket.
func newFirecrackerClient(socketPath string) *firecrackerClient {
	c := &firecrackerClient{socketPath: socketPath}
	transport := &http.Transport{
		DialContext: c.dialContext,
		// Keep the connection alive between stages, so no stage re-dials.
		IdleConnTimeout:     0,
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
	}
	c.httpClient = &http.Client{Transport: transport}
	return c
}

// dialContext hands over the connection connect() made, or dials again if it
// was already used.
func (c *firecrackerClient) dialContext(ctx context.Context, _, _ string) (net.Conn, error) {
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

// connect blocks until the Firecracker API socket accepts connections, or the
// timeout elapses. It keeps the connection for the requests that follow.
func (c *firecrackerClient) connect(timeout time.Duration) error {
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
		// Firecracker binds the socket within about 1ms of starting.
		time.Sleep(1 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = context.DeadlineExceeded
	}
	return fmt.Errorf("firecracker API socket %q not ready within %s: %w", c.socketPath, timeout, lastErr)
}

func (c *firecrackerClient) putMachineConfig(ctx context.Context, machine FirecrackerMachine) error {
	return c.put(ctx, "/machine-config", machine)
}

// putNetworkIface attaches one network interface. Firecracker opens the tap
// device during this call, so it must not be sent before the tap exists.
func (c *firecrackerClient) putNetworkIface(ctx context.Context, iface FirecrackerNet) error {
	return c.put(ctx, "/network-interfaces/"+iface.IfaceID, iface)
}

func (c *firecrackerClient) putDrives(ctx context.Context, drives []FirecrackerDrive) error {
	for _, drive := range drives {
		if err := c.put(ctx, "/drives/"+drive.DriveID, drive); err != nil {
			return err
		}
	}
	return nil
}

func (c *firecrackerClient) putBootSource(ctx context.Context, source FirecrackerBootSource) error {
	return c.put(ctx, "/boot-source", source)
}

func (c *firecrackerClient) putVSock(ctx context.Context, vsock FirecrackerVSockDev) error {
	if vsock.UDSPath == "" {
		return nil
	}
	return c.put(ctx, "/vsock", vsock)
}

// startGuest boots the guest. Firecracker accepts this only once.
func (c *firecrackerClient) startGuest(ctx context.Context) error {
	return c.put(ctx, "/actions", map[string]string{"action_type": "InstanceStart"})
}

func (c *firecrackerClient) put(ctx context.Context, path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s request: %w", path, err)
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
