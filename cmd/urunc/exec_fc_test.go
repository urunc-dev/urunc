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
	"bufio"
	"io"
	"net"
	"testing"
	"time"
)

// TestFCVsockConnectHandshake checks that hybridVsockConnect sends firecracker's
// "CONNECT <port>" line, accepts the "OK" reply, and leaves the bytes that
// follow the reply on the connection for the session to read.
func TestFCVsockConnectHandshake(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	defer client.Close()

	go func() {
		defer server.Close()
		br := bufio.NewReader(server)
		line, _ := br.ReadString('\n')
		if line != "CONNECT 1024\n" {
			t.Errorf("firecracker got %q, want %q", line, "CONNECT 1024\n")
		}
		_, _ = io.WriteString(server, "OK 12345\n")
		// The first session bytes right after the reply must survive.
		_, _ = io.WriteString(server, "SESSION")
	}()

	if err := hybridVsockConnect(client, 1024); err != nil {
		t.Fatalf("hybridVsockConnect: %v", err)
	}

	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, len("SESSION"))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("reading session bytes after handshake: %v", err)
	}
	if string(buf) != "SESSION" {
		t.Fatalf("session bytes after handshake = %q, want %q (reply parser must not swallow them)", buf, "SESSION")
	}
}

// TestFCVsockConnectRefused checks that a non-OK reply is reported as an error.
func TestFCVsockConnectRefused(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	defer client.Close()

	go func() {
		defer server.Close()
		br := bufio.NewReader(server)
		_, _ = br.ReadString('\n')
		_, _ = io.WriteString(server, "ERROR Connection refused\n")
	}()

	if err := hybridVsockConnect(client, 1024); err == nil {
		t.Fatal("expected an error for a non-OK reply, got nil")
	}
}
