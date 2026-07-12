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
	"time"
)

// vmmAPIClient is a minimal HTTP-over-unix-socket client used to drive the
// control API of a running VMM (Firecracker or Cloud Hypervisor). Both VMMs
// speak plain HTTP/1.1 over a unix socket, so a single client covers both.
type vmmAPIClient struct {
	sockPath string
	httpc    *http.Client
}

func newVMMAPIClient(sockPath string) *vmmAPIClient {
	return &vmmAPIClient{
		sockPath: sockPath,
		httpc: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", sockPath)
				},
			},
			Timeout: 60 * time.Second,
		},
	}
}

// request performs an HTTP request against the VMM API socket. A nil body
// sends an empty request. Any non-2xx response is returned as an error
// containing the response body.
func (c *vmmAPIClient) request(method string, path string, body any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal VMM API request body: %w", err)
		}
		rdr = bytes.NewReader(data)
	}

	// The host part of the URL is ignored; the transport always dials the
	// unix socket. "localhost" keeps net/http happy.
	req, err := http.NewRequest(method, "http://localhost"+path, rdr)
	if err != nil {
		return fmt.Errorf("failed to create VMM API request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("VMM API request %s %s failed: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("VMM API request %s %s returned %d: %s",
			method, path, resp.StatusCode, string(respBody))
	}

	return nil
}

// waitForSocket waits until the VMM API socket accepts connections or the
// timeout expires. It is used right after spawning a VMM process to make
// sure the control API is up before issuing requests.
func (c *vmmAPIClient) waitForSocket(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("unix", c.sockPath, time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for VMM API socket %s: %w", c.sockPath, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
