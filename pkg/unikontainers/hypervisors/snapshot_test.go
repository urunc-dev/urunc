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
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// capturedRequest records one HTTP request received by the fake VMM API.
type capturedRequest struct {
	Method string
	Path   string
	Body   string
}

// fakeVMMAPI is a minimal HTTP server listening on a unix socket, standing in
// for the Firecracker / Cloud Hypervisor control API.
type fakeVMMAPI struct {
	sockPath string
	mu       sync.Mutex
	requests []capturedRequest
	status   int
}

func startFakeVMMAPI(t *testing.T, status int) *fakeVMMAPI {
	t.Helper()
	f := &fakeVMMAPI{
		sockPath: filepath.Join(t.TempDir(), "api.sock"),
		status:   status,
	}
	ln, err := net.Listen("unix", f.sockPath)
	require.NoError(t, err)

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.requests = append(f.requests, capturedRequest{
				Method: r.Method,
				Path:   r.URL.Path,
				Body:   string(body),
			})
			f.mu.Unlock()
			w.WriteHeader(f.status)
			if f.status >= 400 {
				_, _ = w.Write([]byte("fake VMM error"))
			}
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return f
}

func (f *fakeVMMAPI) captured() []capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capturedRequest(nil), f.requests...)
}

func TestFirecrackerBuildExecCmdEnablesAPISocket(t *testing.T) {
	fc := &Firecracker{binary: FirecrackerBinary, binaryPath: "/usr/bin/firecracker"}
	args := types.ExecArgs{
		UnikernelPath: testKernelPath,
		Command:       testCommand,
		Seccomp:       true,
	}
	cmd, err := fc.BuildExecCmd(args, &fakeUnikernel{})
	require.NoError(t, err)
	joined := strings.Join(cmd, " ")
	assert.Contains(t, joined, "--api-sock "+InNsAPISockPath)
	assert.NotContains(t, joined, "--no-api")
}

func TestFirecrackerBuildRestoreCmd(t *testing.T) {
	fc := &Firecracker{binary: FirecrackerBinary, binaryPath: "/usr/bin/firecracker"}

	cmd, err := fc.BuildRestoreCmd(types.ExecArgs{Seccomp: true}, InNsSnapshotDir)
	require.NoError(t, err)
	assert.Equal(t, []string{"/usr/bin/firecracker", "--api-sock", InNsAPISockPath}, cmd)

	cmd, err = fc.BuildRestoreCmd(types.ExecArgs{Seccomp: false}, InNsSnapshotDir)
	require.NoError(t, err)
	assert.Contains(t, cmd, "--no-seccomp")
	// A restore launch must not pre-configure boot resources.
	assert.NotContains(t, strings.Join(cmd, " "), "--config-file")
}

func TestFirecrackerSnapshotAPIRequests(t *testing.T) {
	fc := &Firecracker{binary: FirecrackerBinary, binaryPath: "/usr/bin/firecracker"}
	api := startFakeVMMAPI(t, http.StatusNoContent)

	require.NoError(t, fc.PauseVM(api.sockPath))
	require.NoError(t, fc.SnapshotVM(api.sockPath, InNsSnapshotDir))
	require.NoError(t, fc.ResumeVM(api.sockPath))
	require.NoError(t, fc.FinishRestore(api.sockPath, InNsSnapshotDir, NetOverride{
		IfaceID: "net1",
		TapDev:  "tap0_urunc",
	}))

	reqs := api.captured()
	require.Len(t, reqs, 4)

	assert.Equal(t, "PATCH", reqs[0].Method)
	assert.Equal(t, "/vm", reqs[0].Path)
	assert.JSONEq(t, `{"state":"Paused"}`, reqs[0].Body)

	assert.Equal(t, "PUT", reqs[1].Method)
	assert.Equal(t, "/snapshot/create", reqs[1].Path)
	assert.JSONEq(t, `{
		"snapshot_type": "Full",
		"snapshot_path": "/urunc-snapshot/vmstate",
		"mem_file_path": "/urunc-snapshot/memory"
	}`, reqs[1].Body)

	assert.Equal(t, "PATCH", reqs[2].Method)
	assert.JSONEq(t, `{"state":"Resumed"}`, reqs[2].Body)

	assert.Equal(t, "PUT", reqs[3].Method)
	assert.Equal(t, "/snapshot/load", reqs[3].Path)
	assert.JSONEq(t, `{
		"snapshot_path": "/urunc-snapshot/vmstate",
		"mem_backend": {"backend_type": "File", "backend_path": "/urunc-snapshot/memory"},
		"resume_vm": true,
		"network_overrides": [{"iface_id": "net1", "host_dev_name": "tap0_urunc"}]
	}`, reqs[3].Body)
}

func TestCloudHypervisorBuildExecCmdEnablesAPISocket(t *testing.T) {
	ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: "/usr/bin/cloud-hypervisor"}
	args := types.ExecArgs{
		UnikernelPath: testKernelPath,
		Command:       testCommand,
		Seccomp:       true,
	}
	cmd, err := ch.BuildExecCmd(args, &fakeUnikernel{})
	require.NoError(t, err)
	joined := strings.Join(cmd, " ")
	assert.Contains(t, joined, "--api-socket "+InNsAPISockPath)
}

func TestCloudHypervisorBuildRestoreCmd(t *testing.T) {
	ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: "/usr/bin/cloud-hypervisor"}
	cmd, err := ch.BuildRestoreCmd(types.ExecArgs{Seccomp: true}, InNsSnapshotDir)
	require.NoError(t, err)
	joined := strings.Join(cmd, " ")
	assert.Contains(t, joined, "--api-socket "+InNsAPISockPath)
	assert.Contains(t, joined, "--restore source_url=file://"+InNsSnapshotDir)
	assert.Contains(t, joined, "--seccomp true")
}

func TestCloudHypervisorSnapshotAPIRequests(t *testing.T) {
	ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: "/usr/bin/cloud-hypervisor"}
	api := startFakeVMMAPI(t, http.StatusNoContent)

	require.NoError(t, ch.PauseVM(api.sockPath))
	require.NoError(t, ch.SnapshotVM(api.sockPath, InNsSnapshotDir))
	require.NoError(t, ch.FinishRestore(api.sockPath, InNsSnapshotDir, NetOverride{}))

	reqs := api.captured()
	require.Len(t, reqs, 3)
	assert.Equal(t, "/api/v1/vm.pause", reqs[0].Path)
	assert.Equal(t, "/api/v1/vm.snapshot", reqs[1].Path)
	assert.JSONEq(t, `{"destination_url":"file:///urunc-snapshot"}`, reqs[1].Body)
	assert.Equal(t, "/api/v1/vm.resume", reqs[2].Path)
}

func TestCloudHypervisorPrepareRestoreRewritesTap(t *testing.T) {
	ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: "/usr/bin/cloud-hypervisor"}
	dir := t.TempDir()
	config := map[string]any{
		"cpus": map[string]any{"boot_vcpus": 1},
		"net": []any{
			map[string]any{
				"tap": "tap3_urunc",
				"mac": "aa:bb:cc:dd:ee:ff",
			},
		},
	}
	data, err := json.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, CHSnapshotConfigFile), data, 0o644))

	err = ch.PrepareRestore(dir, NetOverride{IfaceID: "net1", TapDev: "tap0_urunc"})
	require.NoError(t, err)

	patched, err := os.ReadFile(filepath.Join(dir, CHSnapshotConfigFile))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(patched, &got))
	netDev := got["net"].([]any)[0].(map[string]any)
	assert.Equal(t, "tap0_urunc", netDev["tap"])
	// The guest-visible MAC is frozen in the snapshot and must not change.
	assert.Equal(t, "aa:bb:cc:dd:ee:ff", netDev["mac"])
}

func TestCloudHypervisorPrepareRestoreNoNet(t *testing.T) {
	ch := &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: "/usr/bin/cloud-hypervisor"}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, CHSnapshotConfigFile), []byte(`{"cpus":{}}`), 0o644))

	// A snapshot without network devices needs no rewrite and must not fail.
	assert.NoError(t, ch.PrepareRestore(dir, NetOverride{TapDev: "tap0_urunc"}))
	// An empty override means nothing to rewrite, even without a config file.
	assert.NoError(t, ch.PrepareRestore(t.TempDir(), NetOverride{}))
}

func TestVMMAPIClientErrorStatus(t *testing.T) {
	fc := &Firecracker{binary: FirecrackerBinary, binaryPath: "/usr/bin/firecracker"}
	api := startFakeVMMAPI(t, http.StatusBadRequest)

	err := fc.PauseVM(api.sockPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "fake VMM error")
}

func TestWaitForVMMAPI(t *testing.T) {
	api := startFakeVMMAPI(t, http.StatusNoContent)
	assert.NoError(t, WaitForVMMAPI(api.sockPath, time.Second))

	missing := filepath.Join(t.TempDir(), "missing.sock")
	err := WaitForVMMAPI(missing, 100*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}
