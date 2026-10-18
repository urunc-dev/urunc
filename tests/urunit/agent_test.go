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

// These tests drive the C agent with the host-side client of
// pkg/agentproto, over a unix socket (URUNIT_AGENT_TRANSPORT=unix:<path>).
// They mirror cmd/urunit-agent/agent_e2e_test.go, so the two suites read
// alike. Tests that switch users or check the owner of a pty call
// requireRoot. Everything else passes as a plain user.

import (
	"bufio"
	"bytes"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/urunc-dev/urunc/pkg/agentproto"
	"golang.org/x/sys/unix"
)

// testDeadline bounds every wait of a test on the agent.
const testDeadline = 15 * time.Second

// agentBin is the urunit binary under test.
var agentBin string

// TestMain runs the tests against the binary URUNIT_BIN names, and skips
// them when it is unset.
func TestMain(m *testing.M) {
	agentBin = os.Getenv("URUNIT_BIN")
	if agentBin == "" {
		fmt.Fprintln(os.Stderr, "URUNIT_BIN is unset, skipping (make test_urunit sets it)")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// requireRoot skips the test unless it runs as root.
func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root: setuid/chown")
	}
}

// startAgent starts the binary in agent mode on the given transport and
// kills it when the test ends. env is added to the test's environment. The
// agent log goes to stderr, or to the test log on failure when stderr is
// nil.
func startAgent(t *testing.T, transport string, env []string, stderr *os.File, extraFiles ...*os.File) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(agentBin)
	cmd.Env = append(os.Environ(), "URUNIT_MODE=agent", "URUNIT_AGENT_TRANSPORT="+transport)
	cmd.Env = append(cmd.Env, env...)
	cmd.ExtraFiles = extraFiles
	var log bytes.Buffer
	if stderr != nil {
		cmd.Stderr = stderr
	} else {
		cmd.Stderr = &log
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start agent: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() && log.Len() > 0 {
			t.Logf("agent log:\n%s", log.String())
		}
	})
	return cmd
}

// agentSocketPath returns a socket path in a new temporary directory. The
// directory is short, since a unix socket path holds at most 107 bytes.
func agentSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "urunit-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "agent.sock")
}

// dialAgent connects to the agent's unix socket at sockPath. It retries for
// up to 5 s while the agent starts.
func dialAgent(t *testing.T, sockPath string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("unix", sockPath)
		if err == nil {
			t.Cleanup(func() { _ = conn.Close() })
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial %s: %v", sockPath, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// newAgent starts an agent on a fresh unix socket and returns its path.
func newAgent(t *testing.T) string {
	t.Helper()
	sock := agentSocketPath(t)
	startAgent(t, "unix:"+sock, nil, nil)
	return sock
}

// newAgentConn starts an agent on a fresh unix socket and connects to it.
func newAgentConn(t *testing.T) net.Conn {
	t.Helper()
	return dialAgent(t, newAgent(t))
}

// sessionResult is what the agent reported for one stream.
type sessionResult struct {
	stdout, stderr bytes.Buffer
	// stdoutFrames and stderrFrames count the output frames.
	stdoutFrames, stderrFrames int
	code                       int
	exited                     bool
	// errMsg is the message of an Error frame. An Error frame ends the
	// stream like an Exit frame does.
	errMsg string
	failed bool
}

func (r *sessionResult) done() bool { return r.exited || r.failed }

// add records frame f in r. It fails on a malformed control payload.
func (r *sessionResult) add(f agentproto.Frame) error {
	switch f.Type {
	case agentproto.TypeStdout:
		r.stdout.Write(f.Payload)
		r.stdoutFrames++
	case agentproto.TypeStderr:
		r.stderr.Write(f.Payload)
		r.stderrFrames++
	case agentproto.TypeError:
		var e agentproto.Error
		if err := json.Unmarshal(f.Payload, &e); err != nil {
			return fmt.Errorf("stream %d: bad error payload %q: %w", f.Stream, f.Payload, err)
		}
		r.errMsg = e.Message
		r.failed = true
	case agentproto.TypeExit:
		var ex agentproto.Exit
		if err := json.Unmarshal(f.Payload, &ex); err != nil {
			return fmt.Errorf("stream %d: bad exit payload %q: %w", f.Stream, f.Payload, err)
		}
		r.code = ex.Code
		r.exited = true
	default:
		return fmt.Errorf("stream %d: unexpected frame type %d", f.Stream, f.Type)
	}
	return nil
}

// readStreams reads frames from conn until every stream in res has ended.
// A frame after the end of its stream, or of a stream not in res, is an
// error.
func readStreams(conn net.Conn, res map[uint32]*sessionResult) error {
	_ = conn.SetReadDeadline(time.Now().Add(testDeadline))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	open := 0
	for _, r := range res {
		if !r.done() {
			open++
		}
	}
	for open > 0 {
		f, err := agentproto.ReadFrame(conn)
		if err != nil {
			return fmt.Errorf("read frame: %w", err)
		}
		r, ok := res[f.Stream]
		if !ok {
			return fmt.Errorf("frame type %d on unknown stream %d", f.Type, f.Stream)
		}
		if r.done() {
			return fmt.Errorf("frame type %d on stream %d after its end", f.Type, f.Stream)
		}
		if err := r.add(f); err != nil {
			return err
		}
		if r.done() {
			open--
		}
	}
	return nil
}

// doSession opens one exec session on stream and reads frames until the
// stream ends. Unlike runSession, it returns its failures, so goroutines
// can call it. An Error frame is an error.
func doSession(conn net.Conn, stream uint32, req agentproto.OpenRequest) (*sessionResult, error) {
	if err := agentproto.WriteJSON(conn, agentproto.TypeOpen, stream, req); err != nil {
		return nil, fmt.Errorf("send open: %w", err)
	}
	r := &sessionResult{}
	if err := readStreams(conn, map[uint32]*sessionResult{stream: r}); err != nil {
		return nil, err
	}
	if r.failed {
		return nil, fmt.Errorf("agent error on stream %d: %s", stream, r.errMsg)
	}
	return r, nil
}

// runSession opens one exec session on stream and reads frames until the
// agent reports Exit. It returns the collected stdout, stderr and exit
// code. An Error frame fails the test.
func runSession(t *testing.T, conn net.Conn, stream uint32, req agentproto.OpenRequest) (stdout, stderr string, code int) {
	t.Helper()
	r, err := doSession(conn, stream, req)
	if err != nil {
		t.Fatal(err)
	}
	return r.stdout.String(), r.stderr.String(), r.code
}

// openError sends an Open frame with payload on stream and returns the
// message of the Error frame that ends it. An Exit frame fails the test.
func openError(t *testing.T, conn net.Conn, stream uint32, payload []byte) string {
	t.Helper()
	if err := agentproto.WriteFrame(conn, agentproto.TypeOpen, stream, payload); err != nil {
		t.Fatalf("send open: %v", err)
	}
	r := &sessionResult{}
	if err := readStreams(conn, map[uint32]*sessionResult{stream: r}); err != nil {
		t.Fatal(err)
	}
	if !r.failed {
		t.Fatalf("stream %d: got Exit %d (stdout %q), want an Error frame", stream, r.code, r.stdout.String())
	}
	return r.errMsg
}

// openRequestError is openError for an OpenRequest.
func openRequestError(t *testing.T, conn net.Conn, stream uint32, req agentproto.OpenRequest) string {
	t.Helper()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return openError(t, conn, stream, payload)
}

// readUntil reads frames of stream from conn into r until cond holds for
// the stdout read so far, or the stream ends.
func readUntil(t *testing.T, conn net.Conn, stream uint32, r *sessionResult, cond func(string) bool) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(testDeadline))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	for !cond(r.stdout.String()) {
		if r.done() {
			t.Fatalf("stream %d ended (exit %d, error %q) before the expected output; stdout %q",
				stream, r.code, r.errMsg, r.stdout.String())
		}
		f, err := agentproto.ReadFrame(conn)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if f.Stream != stream {
			t.Fatalf("frame type %d on unexpected stream %d", f.Type, f.Stream)
		}
		if err := r.add(f); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAgentBinaryIsStatic checks that the binary is a static ELF. The initrd
// has no dynamic loader, so a PT_INTERP header would make it unusable.
func TestAgentBinaryIsStatic(t *testing.T) {
	f, err := elf.Open(agentBin)
	if err != nil {
		t.Fatalf("open %s: %v", agentBin, err)
	}
	defer f.Close()
	if f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN {
		t.Errorf("ELF type = %v, want an executable", f.Type)
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			t.Errorf("%s has a PT_INTERP header: not a static binary", agentBin)
		}
	}
}

// TestAgentExecNonTTY drives a real exec session end to end: the agent
// spawns /bin/sh, streams stdout and stderr back as separate frame types,
// and reports the child's exit code.
func TestAgentExecNonTTY(t *testing.T) {
	conn := newAgentConn(t)
	stdout, stderr, code := runSession(t, conn, 1, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", "echo out-msg; echo err-msg 1>&2; exit 7"},
	})
	if stdout != "out-msg\n" {
		t.Errorf("stdout = %q, want %q", stdout, "out-msg\n")
	}
	if stderr != "err-msg\n" {
		t.Errorf("stderr = %q, want %q", stderr, "err-msg\n")
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

// TestAgentExecTTY exercises the pty path: stdin, stdout and stderr are one
// terminal, so all output arrives as TypeStdout, and the terminal has the
// size of the Open request. No user is set, so nothing is chowned.
func TestAgentExecTTY(t *testing.T) {
	conn := newAgentConn(t)
	r, err := doSession(conn, 2, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", "tty; stty size; echo on-stderr 1>&2"},
		TTY:  true,
		Rows: 33, Cols: 101,
	})
	if err != nil {
		t.Fatal(err)
	}
	stdout := strings.ReplaceAll(r.stdout.String(), "\r\n", "\n")
	if !regexp.MustCompile(`(?m)^/dev/pts/[0-9]+$`).MatchString(stdout) {
		t.Errorf("tty did not print a /dev/pts path, got %q", stdout)
	}
	if !strings.Contains(stdout, "33 101\n") {
		t.Errorf("stty size does not report 33 101, got %q", stdout)
	}
	if !strings.Contains(stdout, "on-stderr") {
		t.Errorf("stderr of a tty session is not in stdout, got %q", stdout)
	}
	if r.stderrFrames != 0 {
		t.Errorf("tty session sent %d TypeStderr frames (%q), want none", r.stderrFrames, r.stderr.String())
	}
	if r.code != 0 {
		t.Errorf("exit code = %d, want 0", r.code)
	}
}

// TestAgentExecOutputBeforeExit runs short sessions that write their output
// just before they exit, on both the pipe and the pty path. The client ends a
// session on its Exit frame, so the output has to reach it first. The race is
// timing dependent, hence the loop.
func TestAgentExecOutputBeforeExit(t *testing.T) {
	conn := newAgentConn(t)
	for i := uint32(0); i < 50; i++ {
		for _, tty := range []bool{false, true} {
			stream := 100 + 2*i
			if tty {
				stream++
			}
			marker := fmt.Sprintf("marker-%d", stream)
			stdout, _, code := runSession(t, conn, stream, agentproto.OpenRequest{
				Argv: []string{"/bin/echo", marker},
				TTY:  tty,
			})
			if !strings.Contains(stdout, marker) {
				t.Fatalf("session %d (tty=%v) lost its output: got %q, want %q", stream, tty, stdout, marker)
			}
			if code != 0 {
				t.Fatalf("session %d (tty=%v) exit code = %d, want 0", stream, tty, code)
			}
		}
	}
}

// TestAgentExecBackgroundHoldsOutput leaves a process in the background that
// keeps the session's stdout open for 5 seconds. The Exit frame still
// arrives, with the output written before it, once the 2 s drain bound has
// passed.
func TestAgentExecBackgroundHoldsOutput(t *testing.T) {
	conn := newAgentConn(t)
	start := time.Now()
	stdout, _, code := runSession(t, conn, 4, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", "sleep 5 & echo done"},
	})
	elapsed := time.Since(start)
	if !strings.Contains(stdout, "done") {
		t.Errorf("stdout missing marker, got %q", stdout)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if elapsed > 3*time.Second {
		t.Errorf("exit reported after %v, want it within 3s", elapsed)
	}
}

// TestAgentStdinRoundTrip feeds bytes to the child over TypeStdin and reads
// them back, checking the stdin path and CloseStdin handling.
func TestAgentStdinRoundTrip(t *testing.T) {
	conn := newAgentConn(t)
	if err := agentproto.WriteJSON(conn, agentproto.TypeOpen, 3, agentproto.OpenRequest{
		Argv: []string{"/bin/cat"},
	}); err != nil {
		t.Fatalf("open: %v", err)
	}
	// More than one pipe buffer, so the agent queues part of it.
	big := bytes.Repeat([]byte("0123456789abcdef"), 16*1024)
	for _, chunk := range [][]byte{[]byte("ping-pong\n"), big} {
		if err := agentproto.WriteFrame(conn, agentproto.TypeStdin, 3, chunk); err != nil {
			t.Fatalf("stdin: %v", err)
		}
	}
	if err := agentproto.WriteFrame(conn, agentproto.TypeCloseStdin, 3, nil); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	r := &sessionResult{}
	if err := readStreams(conn, map[uint32]*sessionResult{3: r}); err != nil {
		t.Fatal(err)
	}
	if r.failed {
		t.Fatalf("agent error: %s", r.errMsg)
	}
	want := append([]byte("ping-pong\n"), big...)
	if !bytes.Equal(r.stdout.Bytes(), want) {
		t.Errorf("cat echoed %d bytes, want %d (prefix %q)", r.stdout.Len(), len(want), r.stdout.Bytes()[:min(r.stdout.Len(), 32)])
	}
	if r.code != 0 {
		t.Errorf("exit code = %d, want 0", r.code)
	}
}

// TestAgentExecChildHoldsNoConnection checks that a process the agent starts
// holds no descriptor of the agent: no socket, no epoll or signalfd. Only
// 0, 1 and 2 are passed on, and ls opens its own directory as 3.
func TestAgentExecChildHoldsNoConnection(t *testing.T) {
	conn := newAgentConn(t)
	for _, tty := range []bool{false, true} {
		stream := uint32(10)
		if tty {
			stream = 11
		}
		stdout, _, code := runSession(t, conn, stream, agentproto.OpenRequest{
			Argv: []string{"/bin/ls", "-l", "/proc/self/fd"},
			TTY:  tty,
		})
		if code != 0 {
			t.Fatalf("tty=%v: exit code = %d, want 0; output %q", tty, code, stdout)
		}
		seen := map[int]bool{}
		// ls colors its listing on a terminal.
		plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(stdout, "")
		for _, line := range strings.Split(strings.ReplaceAll(plain, "\r", ""), "\n") {
			before, target, ok := strings.Cut(line, " -> ")
			if !ok {
				continue
			}
			fields := strings.Fields(before)
			fd, err := strconv.Atoi(fields[len(fields)-1])
			if err != nil {
				t.Fatalf("tty=%v: cannot parse %q", tty, line)
			}
			seen[fd] = true
			if fd > 2 && (strings.HasPrefix(target, "socket:") || strings.HasPrefix(target, "anon_inode:")) {
				t.Errorf("tty=%v: the child holds fd %d -> %s", tty, fd, target)
			}
		}
		for fd := 0; fd < 3; fd++ {
			if !seen[fd] {
				t.Errorf("tty=%v: fd %d missing from the listing:\n%s", tty, fd, stdout)
			}
		}
	}
}

// TestAgentSignal delivers SIGTERM to a session. The exit code is 128+15.
func TestAgentSignal(t *testing.T) {
	conn := newAgentConn(t)
	for _, tty := range []bool{false, true} {
		stream := uint32(20)
		if tty {
			stream = 21
		}
		start := time.Now()
		if err := agentproto.WriteJSON(conn, agentproto.TypeOpen, stream, agentproto.OpenRequest{
			Argv: []string{"/bin/sleep", "30"},
			TTY:  tty,
		}); err != nil {
			t.Fatalf("open: %v", err)
		}
		// The agent starts the process before it reads the next frame.
		if err := agentproto.WriteJSON(conn, agentproto.TypeSignal, stream, agentproto.Signal{Signal: 15}); err != nil {
			t.Fatalf("signal: %v", err)
		}
		r := &sessionResult{}
		if err := readStreams(conn, map[uint32]*sessionResult{stream: r}); err != nil {
			t.Fatal(err)
		}
		if r.failed {
			t.Fatalf("tty=%v: agent error: %s", tty, r.errMsg)
		}
		if r.code != 143 {
			t.Errorf("tty=%v: exit code = %d, want 143", tty, r.code)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("tty=%v: signalled session ended after %v", tty, elapsed)
		}
	}
}

// TestAgentResize changes the window size of a running tty session.
func TestAgentResize(t *testing.T) {
	conn := newAgentConn(t)
	const stream = 30
	if err := agentproto.WriteJSON(conn, agentproto.TypeOpen, stream, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", "stty size; sleep 1; stty size"},
		TTY:  true,
		Rows: 24, Cols: 80,
	}); err != nil {
		t.Fatalf("open: %v", err)
	}
	r := &sessionResult{}
	readUntil(t, conn, stream, r, func(s string) bool { return strings.Contains(s, "24 80") })
	if err := agentproto.WriteJSON(conn, agentproto.TypeResize, stream, agentproto.Resize{Rows: 50, Cols: 132}); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := readStreams(conn, map[uint32]*sessionResult{stream: r}); err != nil {
		t.Fatal(err)
	}
	out := strings.ReplaceAll(r.stdout.String(), "\r\n", "\n")
	if out != "24 80\n50 132\n" {
		t.Errorf("stty output = %q, want %q", out, "24 80\n50 132\n")
	}
	if r.code != 0 {
		t.Errorf("exit code = %d, want 0", r.code)
	}
}

// passwdEntry returns the fields of the /etc/passwd line of name.
func passwdEntry(t *testing.T, name string) []string {
	t.Helper()
	f, err := os.Open("/etc/passwd")
	if err != nil {
		t.Skipf("no /etc/passwd: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) == 7 && fields[0] == name {
			return fields
		}
	}
	t.Skipf("no user %q in /etc/passwd", name)
	return nil
}

// TestAgentUserSpec runs sessions as another user: by name, by uid:gid, with
// the environment defaults of the user, and with the pty owned by it.
func TestAgentUserSpec(t *testing.T) {
	requireRoot(t)
	nobody := passwdEntry(t, "nobody")
	uid, gid, home, shell := nobody[2], nobody[3], nobody[5], nobody[6]
	if home == "" {
		home = "/"
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	conn := newAgentConn(t)
	// nobody's home may not exist (/nonexistent on Debian), so every
	// session sets the cwd.
	stream := uint32(40)
	run := func(user string, tty bool, env []string, argv ...string) string {
		t.Helper()
		stream++
		stdout, _, code := runSession(t, conn, stream, agentproto.OpenRequest{
			Argv: argv, Env: env, User: user, Cwd: "/", TTY: tty,
		})
		if code != 0 {
			t.Fatalf("user %q: %v: exit code = %d, output %q", user, argv, code, stdout)
		}
		return strings.ReplaceAll(stdout, "\r\n", "\n")
	}

	t.Run("name", func(t *testing.T) {
		got := run("nobody", false, nil, "/bin/sh", "-c", "id -u; id -g")
		if want := uid + "\n" + gid + "\n"; got != want {
			t.Errorf("id under nobody = %q, want %q", got, want)
		}
	})
	t.Run("uid:gid", func(t *testing.T) {
		got := run("65534:0", false, nil, "/bin/sh", "-c", "id -u; id -g")
		if got != "65534\n0\n" {
			t.Errorf("id under 65534:0 = %q, want %q", got, "65534\n0\n")
		}
	})
	t.Run("env defaults", func(t *testing.T) {
		got := run("nobody", false, nil, "/usr/bin/env")
		for _, kv := range []string{"HOME=" + home, "USER=nobody", "LOGNAME=nobody", "SHELL=" + shell} {
			if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(kv) + `$`).MatchString(got) {
				t.Errorf("env lacks %s:\n%s", kv, got)
			}
		}
		// A variable of the request wins over the default.
		got = run("nobody", false, []string{"HOME=/custom"}, "/usr/bin/env")
		if !regexp.MustCompile(`(?m)^HOME=/custom$`).MatchString(got) || strings.Count(got, "HOME=") != 1 {
			t.Errorf("env does not keep the request's HOME:\n%s", got)
		}
	})
	t.Run("pty owner", func(t *testing.T) {
		got := run("nobody", true, nil, "/bin/sh", "-c", `ls -ln "$(tty)"`)
		fields := strings.Fields(got)
		if len(fields) < 4 || fields[2] != uid {
			t.Errorf("pty owner of %q is not uid %s", got, uid)
		}
	})
}

// TestAgentUnknownUser asks for a user that does not exist. The agent
// reports it and starts no process.
func TestAgentUnknownUser(t *testing.T) {
	conn := newAgentConn(t)
	marker := filepath.Join(t.TempDir(), "started")
	msg := openRequestError(t, conn, 1, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", `touch "$1"`, "sh", marker},
		User: "nosuch",
	})
	if want := `unknown user "nosuch"`; msg != want {
		t.Errorf("error = %q, want %q", msg, want)
	}
	msg = openRequestError(t, conn, 2, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", `touch "$1"`, "sh", marker},
		User: "0:nosuchgroup",
	})
	if want := `unknown group "nosuchgroup"`; msg != want {
		t.Errorf("error = %q, want %q", msg, want)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("a process ran for an unknown user or group")
	}
}

// TestAgentUserSwitchDeniedWithoutRoot is the negative of TestAgentUserSpec:
// an agent that is not root cannot switch to root. The session fails to
// start instead of running as the agent's own user.
func TestAgentUserSwitchDeniedWithoutRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("runs only as a plain user")
	}
	conn := newAgentConn(t)
	marker := filepath.Join(t.TempDir(), "started")
	msg := openRequestError(t, conn, 1, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", `touch "$1"`, "sh", marker},
		User: "0:0",
	})
	if want := "start: " + syscall.EPERM.Error(); !strings.EqualFold(msg, want) {
		t.Errorf("error = %q, want %q", msg, want)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("the session ran although the user switch failed")
	}
}

// TestAgentBadOpenStartsNothing gives every refused Open a valid argv that
// would leave a marker file. The Error frame alone does not prove that the
// agent refused: a parser that started the command and also reported the
// error would pass TestAgentBadRequests, so this test looks for the marker.
func TestAgentBadOpenStartsNothing(t *testing.T) {
	conn := newAgentConn(t)
	marker := filepath.Join(t.TempDir(), "started")
	argv := fmt.Sprintf(`["/bin/sh","-c","touch %s"]`, marker)
	cases := []struct {
		name    string
		payload string
	}{
		{"bool rows", `{"argv":` + argv + `,"rows":true}`},
		{"string cols", `{"argv":` + argv + `,"cols":"80"}`},
		{"string tty", `{"argv":` + argv + `,"tty":"yes"}`},
		{"bad env", `{"argv":` + argv + `,"env":"A=b"}`},
		{"truncated", `{"argv":` + argv},
		{"unknown user", `{"argv":` + argv + `,"user":"nosuch"}`},
	}
	for i, tc := range cases {
		msg := openError(t, conn, uint32(200+i), []byte(tc.payload))
		if msg == "" {
			t.Errorf("%s: empty error message", tc.name)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("a process ran for a refused Open")
	}
}

// TestAgentBadRequests sends Open frames the agent must refuse, then checks
// that the connection still serves sessions.
func TestAgentBadRequests(t *testing.T) {
	conn := newAgentConn(t)
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"null argv", `{"argv":null,"tty":false}`, "empty argv"},
		{"empty argv", `{"argv":[],"tty":false}`, "empty argv"},
		{"no argv", `{}`, "empty argv"},
		{"string argv", `{"argv":"sh"}`, "bad open request: argv: want an array of strings at offset 8"},
		{"bool rows", `{"argv":["sh"],"rows":true}`, "bad open request: rows: want a number at offset 22"},
		{"truncated", `{"argv":["sh"]`, "bad open request: unexpected end of JSON, want '}' at offset 14"},
		{"not json", `not json`, "bad open request: invalid literal, want null at offset 0"},
		{"empty payload", ``, "bad open request: unexpected end of JSON, want '{' at offset 0"},
	}
	for i, tc := range cases {
		got := openError(t, conn, uint32(100+i), []byte(tc.payload))
		if got != tc.want {
			t.Errorf("%s: error = %q, want %q", tc.name, got, tc.want)
		}
	}

	// A stream in use cannot be opened again.
	if err := agentproto.WriteJSON(conn, agentproto.TypeOpen, 5, agentproto.OpenRequest{
		Argv: []string{"/bin/sleep", "30"},
	}); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := agentproto.WriteJSON(conn, agentproto.TypeOpen, 5, agentproto.OpenRequest{
		Argv: []string{"/bin/echo", "second"},
	}); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := agentproto.WriteJSON(conn, agentproto.TypeSignal, 5, agentproto.Signal{Signal: 9}); err != nil {
		t.Fatalf("signal: %v", err)
	}
	// The Error frame of the second Open ends stream 5 for the client.
	// The Exit frame of the first session follows it.
	_ = conn.SetReadDeadline(time.Now().Add(testDeadline))
	var errMsg string
	code := -1
	for code < 0 {
		f, err := agentproto.ReadFrame(conn)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if f.Stream != 5 {
			t.Fatalf("frame type %d on unexpected stream %d", f.Type, f.Stream)
		}
		switch f.Type {
		case agentproto.TypeError:
			var e agentproto.Error
			_ = json.Unmarshal(f.Payload, &e)
			errMsg = e.Message
		case agentproto.TypeExit:
			var ex agentproto.Exit
			_ = json.Unmarshal(f.Payload, &ex)
			code = ex.Code
		default:
			t.Fatalf("unexpected frame type %d on stream 5: %q", f.Type, f.Payload)
		}
	}
	if want := "stream 5 already in use"; errMsg != want {
		t.Errorf("duplicate stream error = %q, want %q", errMsg, want)
	}
	if code != 137 {
		t.Errorf("first session exit code = %d, want 137", code)
	}

	stdout, _, code := runSession(t, conn, 6, agentproto.OpenRequest{Argv: []string{"/bin/echo", "still-serving"}})
	if stdout != "still-serving\n" || code != 0 {
		t.Errorf("session after bad requests: stdout %q, code %d", stdout, code)
	}
}

// TestAgentStartFailure checks that an exec or chdir failure is an Error
// frame, never an Exit frame.
func TestAgentStartFailure(t *testing.T) {
	conn := newAgentConn(t)
	enoent := syscall.ENOENT.Error()
	cases := []struct {
		name string
		req  agentproto.OpenRequest
		want string
	}{
		{"missing binary", agentproto.OpenRequest{Argv: []string{"/nonexistent"}}, "start: "},
		{"missing binary tty", agentproto.OpenRequest{Argv: []string{"/nonexistent"}, TTY: true}, "start (tty): "},
		{"missing command", agentproto.OpenRequest{Argv: []string{"no-such-command-xyz"}}, "start: "},
		{"missing cwd", agentproto.OpenRequest{Argv: []string{"/bin/true"}, Cwd: "/nonexistent"}, "start: "},
		{"missing cwd tty", agentproto.OpenRequest{Argv: []string{"/bin/true"}, Cwd: "/nonexistent", TTY: true}, "start (tty): "},
	}
	for i, tc := range cases {
		got := openRequestError(t, conn, uint32(200+i), tc.req)
		if !strings.HasPrefix(got, tc.want) || !strings.EqualFold(strings.TrimPrefix(got, tc.want), enoent) {
			t.Errorf("%s: error = %q, want %q", tc.name, got, tc.want+enoent)
		}
	}
	// readStreams fails on any late frame of the failed streams, so this
	// session also proves none of them sent an Exit frame.
	res := map[uint32]*sessionResult{300: {}}
	for i := range cases {
		res[uint32(200+i)] = &sessionResult{failed: true}
	}
	if err := agentproto.WriteJSON(conn, agentproto.TypeOpen, 300, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", "sleep 0.2; echo ok"},
	}); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := readStreams(conn, res); err != nil {
		t.Fatal(err)
	}
	if res[300].stdout.String() != "ok\n" || res[300].code != 0 {
		t.Errorf("session after start failures: stdout %q, code %d", res[300].stdout.String(), res[300].code)
	}
}

// TestAgentConcurrentSessions runs ten sessions at once on one connection.
// Their frames interleave and each stream gets its own output and code.
func TestAgentConcurrentSessions(t *testing.T) {
	conn := newAgentConn(t)
	res := map[uint32]*sessionResult{}
	for i := uint32(1); i <= 10; i++ {
		script := fmt.Sprintf("for n in 1 2 3; do echo out-%d-$n; echo err-%d-$n 1>&2; sleep 0.0%d; done; exit %d", i, i, i%4, i)
		if err := agentproto.WriteJSON(conn, agentproto.TypeOpen, i, agentproto.OpenRequest{
			Argv: []string{"/bin/sh", "-c", script},
			TTY:  i%3 == 0,
		}); err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		res[i] = &sessionResult{}
	}
	if err := readStreams(conn, res); err != nil {
		t.Fatal(err)
	}
	for i := uint32(1); i <= 10; i++ {
		r := res[i]
		if r.failed {
			t.Errorf("stream %d: agent error %s", i, r.errMsg)
			continue
		}
		out := strings.ReplaceAll(r.stdout.String(), "\r\n", "\n")
		want := fmt.Sprintf("out-%d-1\nout-%d-2\nout-%d-3\n", i, i, i)
		wantErr := fmt.Sprintf("err-%d-1\nerr-%d-2\nerr-%d-3\n", i, i, i)
		if i%3 == 0 {
			// One terminal: both streams in stdout, in order.
			want = fmt.Sprintf("out-%d-1\nerr-%d-1\nout-%d-2\nerr-%d-2\nout-%d-3\nerr-%d-3\n", i, i, i, i, i, i)
			wantErr = ""
		}
		if out != want {
			t.Errorf("stream %d: stdout = %q, want %q", i, out, want)
		}
		if r.stderr.String() != wantErr {
			t.Errorf("stream %d: stderr = %q, want %q", i, r.stderr.String(), wantErr)
		}
		if r.code != int(i) {
			t.Errorf("stream %d: exit code = %d, want %d", i, r.code, i)
		}
	}
}

// TestAgentManyConnections serves five connections at once. Each runs its
// sessions on the same stream numbers as the others.
func TestAgentManyConnections(t *testing.T) {
	sock := newAgent(t)
	const conns, per = 5, 5
	var wg sync.WaitGroup
	errs := make(chan error, conns)
	for c := 0; c < conns; c++ {
		conn := dialAgent(t, sock)
		wg.Add(1)
		go func(c int, conn net.Conn) {
			defer wg.Done()
			for i := 1; i <= per; i++ {
				marker := fmt.Sprintf("conn-%d-session-%d", c, i)
				r, err := doSession(conn, uint32(i), agentproto.OpenRequest{
					Argv: []string{"/bin/sh", "-c", fmt.Sprintf("sleep 0.05; echo %s; exit %d", marker, c+i)},
				})
				if err != nil {
					errs <- fmt.Errorf("conn %d: %w", c, err)
					return
				}
				if r.stdout.String() != marker+"\n" || r.code != c+i {
					errs <- fmt.Errorf("conn %d session %d: stdout %q code %d, want %q code %d",
						c, i, r.stdout.String(), r.code, marker, c+i)
					return
				}
			}
		}(c, conn)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestAgentTTYLastBytesAfterEIO runs short tty sessions whose last bytes
// are still queued on the pty when the slave closes. The master read may
// return EIO before them, and the agent must read again. It mirrors
// cmd/urunit-agent/relay_stress_test.go.
func TestAgentTTYLastBytesAfterEIO(t *testing.T) {
	sock := newAgent(t)
	const workers, per = 8, 125
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		conn := dialAgent(t, sock)
		wg.Add(1)
		go func(w int, conn net.Conn) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				id := w*per + i
				marker := fmt.Sprintf("tail-marker-%d", id)
				r, err := doSession(conn, uint32(id+1), agentproto.OpenRequest{
					Argv: []string{"/bin/sh", "-c", fmt.Sprintf("printf '%s'; exit 3", marker)},
					TTY:  true,
				})
				if err != nil {
					errs <- fmt.Errorf("session %d: %w", id, err)
					return
				}
				if !strings.Contains(r.stdout.String(), marker) || r.code != 3 {
					errs <- fmt.Errorf("session %d: stdout %q code %d, want %q code 3", id, r.stdout.String(), r.code, marker)
					return
				}
			}
		}(w, conn)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// fillPipe writes to the write end of a pipe until it holds no more.
func fillPipe(t *testing.T, w *os.File) int {
	t.Helper()
	rc, err := w.SyscallConn()
	if err != nil {
		t.Fatalf("syscall conn: %v", err)
	}
	total := 0
	var werr error
	err = rc.Control(func(fd uintptr) {
		if werr = unix.SetNonblock(int(fd), true); werr != nil {
			return
		}
		page := bytes.Repeat([]byte{'x'}, 4096)
		for _, chunk := range [][]byte{page, page[:1]} {
			for {
				n, err := unix.Write(int(fd), chunk)
				if errors.Is(err, unix.EINTR) {
					continue
				}
				if errors.Is(err, unix.EAGAIN) {
					break
				}
				if err != nil {
					werr = err
					return
				}
				total += n
			}
		}
	})
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	if werr != nil {
		t.Fatalf("fill pipe: %v", werr)
	}
	return total
}

// TestAgentSessionsIgnoreBlockedConsole points the agent's log at a console
// that is full. The log lines are dropped and the sessions go on. Once the
// console drains, the next line reports how many were dropped.
func TestAgentSessionsIgnoreBlockedConsole(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	filled := fillPipe(t, w)
	sock := agentSocketPath(t)
	startAgent(t, "unix:"+sock, nil, w)
	_ = w.Close()
	conn := dialAgent(t, sock)

	// Each frame of an undefined type is a log line.
	const unknown = 8
	for i := 0; i < unknown; i++ {
		if err := agentproto.WriteFrame(conn, 200, 9, nil); err != nil {
			t.Fatalf("send frame %d: %v", i, err)
		}
	}
	start := time.Now()
	stdout, _, code := runSession(t, conn, 1, agentproto.OpenRequest{Argv: []string{"/bin/echo", "marker"}})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("echo session took %v, want it within 2s", elapsed)
	}
	if stdout != "marker\n" || code != 0 {
		t.Errorf("echo session: stdout %q, code %d", stdout, code)
	}
	// This session logs that its output is still open, right before the
	// Exit frame.
	start = time.Now()
	stdout, _, code = runSession(t, conn, 2, agentproto.OpenRequest{
		Argv: []string{"/bin/sh", "-c", "echo early; sleep 5 &"},
	})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("exit reported after %v, want it within 3s", elapsed)
	}
	if stdout != "early\n" || code != 0 {
		t.Errorf("background session: stdout %q, code %d", stdout, code)
	}

	// Drain the console, then log one more line.
	var mu sync.Mutex
	var console bytes.Buffer
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, err := r.Read(buf)
			mu.Lock()
			console.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	dropped := regexp.MustCompile(`urunit-agent: dropped ([0-9]+) log lines\n`)
	deadline := time.Now().Add(5 * time.Second)
	var m []string
	for m == nil {
		if time.Now().After(deadline) {
			mu.Lock()
			tail := console.Bytes()[min(console.Len(), filled):]
			mu.Unlock()
			t.Fatalf("no dropped log lines report; console after the prefill: %q", tail)
		}
		mu.Lock()
		drained := console.Len() >= filled
		mu.Unlock()
		if drained {
			if err := agentproto.WriteFrame(conn, 201, 9, nil); err != nil {
				t.Fatalf("send frame: %v", err)
			}
		}
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		m = dropped.FindStringSubmatch(console.String()[min(console.Len(), filled):])
		mu.Unlock()
	}
	// The startup line, the unknown frames and the drain message.
	if n, _ := strconv.Atoi(m[1]); n < unknown+1 {
		t.Errorf("dropped %d log lines, want at least %d", n, unknown+1)
	}
}
