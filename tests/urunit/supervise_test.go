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

//go:build linux

package urunit

// These tests run the binary in supervisor mode (URUNIT_MODE=supervise)
// with a URUNIT_CONFIG, an application and the in-process agent. The
// supervisor ends with a reboot. The tests skip when the process may issue
// one, so they can never power off a host.

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urunc-dev/urunc/pkg/agentproto"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// capSysBoot is the capability number of CAP_SYS_BOOT.
const capSysBoot = 22

// requireNoReboot skips the test when this process holds CAP_SYS_BOOT.
func requireNoReboot(t *testing.T) {
	t.Helper()
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Skipf("cannot read the capabilities: %v", err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		v, ok := strings.CutPrefix(l, "CapEff:")
		if !ok {
			continue
		}
		caps, err := strconv.ParseUint(strings.TrimSpace(v), 16, 64)
		if err != nil {
			t.Skipf("cannot parse %q: %v", l, err)
		}
		if caps&(1<<capSysBoot) != 0 {
			t.Skip("CAP_SYS_BOOT is held: the supervisor would reboot")
		}
		return
	}
	t.Skip("no CapEff line in /proc/self/status")
}

// TestSupervisorRunsAppAndAgent checks the supervisor end to end: it reads
// URUNIT_CONFIG, starts the agent before the app, runs the app with the
// environment of the configuration, and asks the agent to exit once the app
// has exited.
func TestSupervisorRunsAppAndAgent(t *testing.T) {
	requireNoReboot(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "urunit.conf")
	marker := filepath.Join(dir, "marker")
	release := filepath.Join(dir, "release")
	sock := agentSocketPath(t)

	// The configuration is what urunc writes: NUL-terminated records, a
	// value with a new line and a variable that starts with the end marker.
	conf := unikernels.BuildUrunitConfig(
		[]string{"SUPERVISED=from-config", "MULTI=line1\nline2", "UEE_MODE=1", "LAST=ok"},
		types.ProcessConfig{UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), WorkDir: dir},
		nil, "qemu")
	if err := os.WriteFile(cfg, []byte(conf), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	script := `printf '%s|%s|%s|%s|%s' "$SUPERVISED" "$MULTI" "$UEE_MODE" "$LAST" "$(pwd)" > "$1.tmp" && mv "$1.tmp" "$1"; ` +
		`while [ ! -e "$2" ]; do sleep 0.05; done; exit 3`
	cmd := exec.Command(agentBin, "/bin/sh", "-c", script, "sh", marker, release)
	cmd.Env = append(os.Environ(),
		"URUNIT_MODE=supervise",
		"URUNIT_CONFIG="+cfg,
		"URUNIT_STAGED_BIN="+agentBin,
		"URUNIT_AGENT_TRANSPORT=unix:"+sock)
	var log strings.Builder
	cmd.Stderr = &log
	if err := cmd.Start(); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		if t.Failed() {
			t.Logf("supervisor log:\n%s", log.String())
		}
	})

	// The app runs with the environment of the configuration.
	if !waitFor(testDeadline, func() bool {
		b, err := os.ReadFile(marker)
		return err == nil && len(b) > 0
	}) {
		t.Fatalf("the app did not run with the environment of URUNIT_CONFIG")
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if want := "from-config|line1\nline2|1|ok|" + dir; string(got) != want {
		t.Fatalf("the app saw %q, want %q", got, want)
	}

	// The agent runs next to the app and serves exec sessions.
	conn := dialAgent(t, sock)
	stdout, _, code := runSession(t, conn, 1, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", "echo agent-up"},
	})
	if stdout != "agent-up\n" || code != 0 {
		t.Fatalf("exec through the agent: stdout %q, exit %d", stdout, code)
	}
	_ = conn.Close()

	// The app exits and the supervisor goes on to the shutdown, which
	// fails without CAP_SYS_BOOT.
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatalf("release the app: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(testDeadline):
		t.Fatalf("the supervisor did not leave after the app exited")
	}
	if !strings.Contains(log.String(), "reboot") {
		t.Errorf("the supervisor did not reach the reboot; log:\n%s", log.String())
	}

	// The agent was asked to exit: it stops accepting connections.
	if !waitFor(5*time.Second, func() bool {
		c, err := net.Dial("unix", sock)
		if err != nil {
			return true
		}
		_ = c.Close()
		return false
	}) {
		t.Errorf("the agent still accepts connections after the app exited")
	}
}

// TestSupervisorWithoutAgent checks that no agent runs when the staged
// binary is missing, and that the app still starts.
func TestSupervisorWithoutAgent(t *testing.T) {
	requireNoReboot(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	sock := agentSocketPath(t)

	cmd := exec.Command(agentBin, "/bin/sh", "-c", `echo ran > "$1"`, "sh", marker)
	cmd.Env = append(os.Environ(),
		"URUNIT_MODE=supervise",
		"URUNIT_STAGED_BIN="+filepath.Join(dir, "missing"),
		"URUNIT_AGENT_TRANSPORT=unix:"+sock)
	_ = cmd.Run() // the shutdown fails without CAP_SYS_BOOT
	if b, err := os.ReadFile(marker); err != nil || strings.TrimSpace(string(b)) != "ran" {
		t.Fatalf("the app did not run: %q, %v", b, err)
	}
	if _, err := os.Stat(sock); err == nil {
		t.Errorf("an agent socket exists without a staged binary")
	}
}
