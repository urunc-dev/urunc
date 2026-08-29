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

type chRecordedRequest struct {
	path string
	body map[string]any
}

type fakeCHAPI struct {
	mu       sync.Mutex
	requests []chRecordedRequest
}

func (f *fakeCHAPI) recorded() []chRecordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]chRecordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

func startFakeCHAPI(t *testing.T) (*fakeCHAPI, string) {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "ch.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on %s: %v", sockPath, err)
	}
	api := &fakeCHAPI{}
	srv := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			api.mu.Lock()
			api.requests = append(api.requests, chRecordedRequest{path: r.URL.Path, body: body})
			api.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return api, sockPath
}

func newTestCHSession(t *testing.T, sockPath string) *CHSession {
	t.Helper()
	client := newCHAPIClient(sockPath)
	if err := client.connect(2 * time.Second); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	return &CHSession{client: client}
}

// TestCHConfigureVMSendsFullConfig verifies vm.create carries the complete
// machine configuration mapped from ExecArgs: payload paths verbatim (no
// rootfs prefixing), memory in bytes, both vcpu fields, net, disks, and the
// fixed console/serial modes.
func TestCHConfigureVMSendsFullConfig(t *testing.T) {
	api, sockPath := startFakeCHAPI(t)
	session := newTestCHSession(t, sockPath)

	args := types.ExecArgs{
		ContainerID:   "test-container",
		Command:       "app -arg",
		UnikernelPath: "/unikernel/app.bin",
		InitrdPath:    "/unikernel/initrd",
		MemSizeB:      256 * 1024 * 1024,
		VCPUs:         2,
	}
	args.Net.TapDev = "tap0"
	args.Net.MAC = "aa:bb:cc:dd:ee:ff"
	args.Net.MTU = 1500
	ukernel := &fakeUnikernel{
		blockCli: []types.MonitorBlockArgs{{ID: "rootfs", Path: "/dev/mapper/test-snap-1"}},
	}

	if err := session.ConfigureVM(context.Background(), args, ukernel); err != nil {
		t.Fatalf("ConfigureVM failed: %v", err)
	}

	reqs := api.recorded()
	if len(reqs) != 1 || reqs[0].path != "/api/v1/vm.create" {
		t.Fatalf("expected one PUT to /api/v1/vm.create, got %+v", reqs)
	}
	body := reqs[0].body

	payload, _ := body["payload"].(map[string]any)
	if payload["kernel"] != "/unikernel/app.bin" || payload["cmdline"] != "app -arg" || payload["initramfs"] != "/unikernel/initrd" {
		t.Errorf("unexpected payload: %v", payload)
	}
	memory, _ := body["memory"].(map[string]any)
	if memory["size"] != float64(256*1024*1024) {
		t.Errorf("memory.size = %v, want bytes not MB", memory["size"])
	}
	cpus, _ := body["cpus"].(map[string]any)
	if cpus["boot_vcpus"] != float64(2) || cpus["max_vcpus"] != float64(2) {
		t.Errorf("unexpected cpus: %v", cpus)
	}
	nets, _ := body["net"].([]any)
	if len(nets) != 1 {
		t.Fatalf("expected one net device, got %v", body["net"])
	}
	net0, _ := nets[0].(map[string]any)
	if net0["tap"] != "tap0" || net0["mac"] != "aa:bb:cc:dd:ee:ff" || net0["mtu"] != float64(1500) {
		t.Errorf("unexpected net[0]: %v", net0)
	}
	disks, _ := body["disks"].([]any)
	if len(disks) != 1 {
		t.Fatalf("expected one disk, got %v", body["disks"])
	}
	disk0, _ := disks[0].(map[string]any)
	if disk0["path"] != "/dev/mapper/test-snap-1" {
		t.Errorf("disk path = %v, want unprefixed", disk0["path"])
	}
	serial, _ := body["serial"].(map[string]any)
	console, _ := body["console"].(map[string]any)
	if serial["mode"] != "Tty" || console["mode"] != "Off" {
		t.Errorf("serial=%v console=%v", serial, console)
	}
}

// TestCHBootAfterCreate verifies the request ordering create then boot.
func TestCHBootAfterCreate(t *testing.T) {
	api, sockPath := startFakeCHAPI(t)
	session := newTestCHSession(t, sockPath)
	ctx := context.Background()

	args := types.ExecArgs{ContainerID: "test-container", Command: "app", UnikernelPath: "/unikernel/app.bin", MemSizeB: 256 * 1024 * 1024, VCPUs: 1}
	if err := session.ConfigureVM(ctx, args, &fakeUnikernel{}); err != nil {
		t.Fatalf("ConfigureVM failed: %v", err)
	}
	if err := session.BootVM(ctx); err != nil {
		t.Fatalf("BootVM failed: %v", err)
	}
	want := []string{"/api/v1/vm.create", "/api/v1/vm.boot"}
	reqs := api.recorded()
	if len(reqs) != len(want) {
		t.Fatalf("expected %v, got %+v", want, reqs)
	}
	for i := range want {
		if reqs[i].path != want[i] {
			t.Errorf("request %d: got %s, want %s", i, reqs[i].path, want[i])
		}
	}
}

// TestCHConfigureVMRejectsRawCliOverrides verifies the documented limitation:
// unikernel-provided raw CLI argument strings cannot be mapped to the REST
// config and must fail loudly rather than be silently dropped.
func TestCHConfigureVMRejectsRawCliOverrides(t *testing.T) {
	_, sockPath := startFakeCHAPI(t)
	session := newTestCHSession(t, sockPath)

	args := types.ExecArgs{ContainerID: "test-container", Command: "app", UnikernelPath: "/unikernel/app.bin", MemSizeB: 256 * 1024 * 1024, VCPUs: 1}
	ukernel := &fakeUnikernel{
		blockCli: []types.MonitorBlockArgs{{ID: "vol0", ExactArgs: "--disk path=/x"}},
	}
	if err := session.ConfigureVM(context.Background(), args, ukernel); err == nil {
		t.Fatal("ConfigureVM accepted a raw CLI override it cannot map")
	}
}
