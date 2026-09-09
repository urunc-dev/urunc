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
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// socketRequestTimeout bounds a single control-socket REST request so a stuck
// monitor cannot block the kill path indefinitely.
const socketRequestTimeout = 5 * time.Second

// unixSocketRequest sends an HTTP request to a monitor's REST API over a unix
// socket. The URL host is a placeholder that net/http requires.
func unixSocketRequest(socketPath, method, urlPath string, body []byte) error {
	client := &http.Client{
		Timeout: socketRequestTimeout,
		Transport: &http.Transport{
			// One-shot request, so leave no idle connection behind.
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://unix"+urlPath, reqBody)
	if err != nil {
		return fmt.Errorf("failed to build %s %s request: %w", method, urlPath, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// A failed client.Do covers both a socket we could not reach and a
	// monitor that never answered in time; either way, the request never
	// completed.
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w (%s %s over %q): %w",
			ErrShutdownConnect, method, urlPath, socketPath, err)
	}
	defer resp.Body.Close()

	// The body is only used to enrich an error string, so cap the read.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	// A status code proves the monitor is alive and answered, so a non-2xx
	// here is the monitor rejecting the request, not an unreachable monitor.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: %s %s returned status %d: %s",
			ErrShutdownRefused, method, urlPath, resp.StatusCode,
			strings.TrimSpace(string(respBody)))
	}
	return nil
}
