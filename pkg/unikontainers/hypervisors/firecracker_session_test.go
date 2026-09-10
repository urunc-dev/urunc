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
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// recordedRequest is one PUT the fake Firecracker API received.
type recordedRequest struct {
	path string
	body map[string]any
}

// fakeFirecrackerAPI records every request the client sends, in order.
type fakeFirecrackerAPI struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (f *fakeFirecrackerAPI) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// startFakeFirecrackerAPI serves a fake Firecracker API on a Unix socket in a
// temporary directory and returns the recorder plus the socket path.
func startFakeFirecrackerAPI(t *testing.T) (*fakeFirecrackerAPI, string) {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "fc.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on %s: %v", sockPath, err)
	}

	api := &fakeFirecrackerAPI{}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			api.mu.Lock()
			api.requests = append(api.requests, recordedRequest{path: r.URL.Path, body: body})
			api.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}),
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return api, sockPath
}

// newTestSession connects a client to the fake API and wraps it in a session.
func newTestSession(t *testing.T, sockPath string) *FirecrackerSession {
	t.Helper()
	client := newFirecrackerClient(sockPath)
	if err := client.connect(2 * time.Second); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	return &FirecrackerSession{client: client}
}

// TestConfigureGuestSendsUnprefixedPaths verifies that the session sends the
// kernel, initrd and drive paths exactly as given: the child runs inside the
// monitor rootfs, so no path may be prefixed with a host-side rootfs
// directory.
func TestConfigureGuestSendsUnprefixedPaths(t *testing.T) {
	api, sockPath := startFakeFirecrackerAPI(t)
	session := newTestSession(t, sockPath)

	args := types.ExecArgs{
		ContainerID:   "test-container",
		Command:       "app -arg",
		UnikernelPath: "/unikernel/app.bin",
		InitrdPath:    "/unikernel/initrd",
		MemSizeB:      256 * 1024 * 1024,
		VCPUs:         1,
	}
	ukernel := &fakeUnikernel{
		blockCli: []types.MonitorBlockArgs{
			{ID: "rootfs", Path: "/dev/mapper/test-snap-1"},
		},
	}

	if err := session.ConfigureGuest(context.Background(), args, ukernel); err != nil {
		t.Fatalf("ConfigureGuest failed: %v", err)
	}

	reqs := api.recorded()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests (drive, boot-source), got %d: %+v", len(reqs), reqs)
	}

	drive := reqs[0]
	if drive.path != "/drives/rootfs" {
		t.Errorf("expected first PUT to /drives/rootfs, got %s", drive.path)
	}
	if got := drive.body["path_on_host"]; got != "/dev/mapper/test-snap-1" {
		t.Errorf("drive path_on_host = %v, want unprefixed /dev/mapper/test-snap-1", got)
	}

	boot := reqs[1]
	if boot.path != "/boot-source" {
		t.Errorf("expected second PUT to /boot-source, got %s", boot.path)
	}
	if got := boot.body["kernel_image_path"]; got != "/unikernel/app.bin" {
		t.Errorf("kernel_image_path = %v, want unprefixed /unikernel/app.bin", got)
	}
	if got := boot.body["initrd_path"]; got != "/unikernel/initrd" {
		t.Errorf("initrd_path = %v, want unprefixed /unikernel/initrd", got)
	}
}

// TestConfigureGuestSendsUnprefixedVSockPath verifies the vsock device, when
// enabled, is sent with its socket path exactly as given, with no rootfs
// prefixing, and after the boot source.
func TestConfigureGuestSendsUnprefixedVSockPath(t *testing.T) {
	api, sockPath := startFakeFirecrackerAPI(t)
	session := newTestSession(t, sockPath)

	args := types.ExecArgs{
		ContainerID:   "test-container",
		Command:       "app",
		UnikernelPath: "/unikernel/app.bin",
		MemSizeB:      256 * 1024 * 1024,
		VCPUs:         1,
		VAccelType:    "vsock",
		VSockDevPath:  "/tmp/vaccel",
		VSockDevID:    3,
	}
	if err := session.ConfigureGuest(context.Background(), args, &fakeUnikernel{}); err != nil {
		t.Fatalf("ConfigureGuest failed: %v", err)
	}

	reqs := api.recorded()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests (boot-source, vsock), got %d: %+v", len(reqs), reqs)
	}
	vsock := reqs[1]
	if vsock.path != "/vsock" {
		t.Errorf("expected last PUT to /vsock, got %s", vsock.path)
	}
	if got := vsock.body["uds_path"]; got != "/tmp/vaccel/vaccel.sock" {
		t.Errorf("uds_path = %v, want unprefixed /tmp/vaccel/vaccel.sock", got)
	}
	if got := vsock.body["guest_cid"]; got != float64(3) {
		t.Errorf("guest_cid = %v, want 3", got)
	}
}

// TestSessionSendsStagesInOrder verifies the full post-pivot sequence lands on
// the API in the order Firecracker requires: machine-config, network
// interface, boot source, and InstanceStart last.
func TestSessionSendsStagesInOrder(t *testing.T) {
	api, sockPath := startFakeFirecrackerAPI(t)
	session := newTestSession(t, sockPath)
	ctx := context.Background()

	args := types.ExecArgs{
		ContainerID:   "test-container",
		Command:       "app",
		UnikernelPath: "/unikernel/app.bin",
		MemSizeB:      256 * 1024 * 1024,
		VCPUs:         1,
	}
	if err := session.ConfigureMachine(ctx, args); err != nil {
		t.Fatalf("ConfigureMachine failed: %v", err)
	}
	netParams := types.NetDevParams{TapDev: "tap0", MAC: "aa:bb:cc:dd:ee:ff"}
	if err := session.ConfigureNetwork(ctx, netParams); err != nil {
		t.Fatalf("ConfigureNetwork failed: %v", err)
	}
	if err := session.ConfigureGuest(ctx, args, &fakeUnikernel{}); err != nil {
		t.Fatalf("ConfigureGuest failed: %v", err)
	}
	if err := session.StartGuest(ctx); err != nil {
		t.Fatalf("StartGuest failed: %v", err)
	}

	want := []string{
		"/machine-config",
		"/network-interfaces/net1",
		"/boot-source",
		"/actions",
	}
	reqs := api.recorded()
	if len(reqs) != len(want) {
		t.Fatalf("expected %d requests, got %d: %+v", len(want), len(reqs), reqs)
	}
	for i, w := range want {
		if reqs[i].path != w {
			t.Errorf("request %d: got %s, want %s", i, reqs[i].path, w)
		}
	}
}
