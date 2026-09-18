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
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"

	"github.com/urunc-dev/urunc/pkg/agentproto"
)

// execCommand runs an additional process inside a running urunc guest by
// talking to the in-guest urunit-agent over vsock (the guest CID, agent port),
// the same transport hull uses. It follows the runc exec CLI (containerd
// invokes `urunc exec --process <file> <id>`).
var execCommand = &cli.Command{
	Name:      "exec",
	Usage:     "execute a new process inside the container",
	ArgsUsage: `<container-id> [command...]`,
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "process", Aliases: []string{"p"}, Usage: "path to a process.json describing the process to exec"},
		&cli.StringFlag{Name: "console-socket", Usage: "path to a socket that receives the pty master fd (tty sessions)"},
		&cli.StringFlag{Name: "pid-file", Usage: "where to write the process pid"},
		&cli.StringFlag{Name: "cwd", Usage: "working directory for the process"},
		&cli.StringSliceFlag{Name: "env", Aliases: []string{"e"}, Usage: "environment variables"},
		&cli.StringFlag{Name: "user", Aliases: []string{"u"}, Usage: "user (uid[:gid]) to run as"},
		&cli.BoolFlag{Name: "tty", Aliases: []string{"t"}, Usage: "allocate a pseudo-TTY"},
		&cli.BoolFlag{Name: "detach", Aliases: []string{"d"}, Usage: "detach from the process"},
		// Accepted for runc compatibility; not meaningful for a guest exec.
		&cli.BoolFlag{Name: "no-new-privs"},
		&cli.IntFlag{Name: "preserve-fds"},
		&cli.StringSliceFlag{Name: "cap"},
		&cli.StringFlag{Name: "apparmor"},
		&cli.StringFlag{Name: "process-label"},
	},
	Action: func(_ context.Context, cmd *cli.Command) error {
		logrus.WithField("command", "EXEC").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, minArgs); err != nil {
			return err
		}
		return execAction(cmd)
	},
}

// execChildEnv marks the re-exec'd proxy child so it runs the session instead
// of daemonizing again. execProcessEnv carries the process spec to that child,
// because containerd deletes its own --process temp file as soon as this
// (parent) call returns, before the detached child would read it. execReadyEnv
// and execParentEnv name the fds of the two handshake pipes between the parent
// and the child (see execReady).
const (
	execChildEnv   = "_URUNC_EXEC_PROXY"
	execProcessEnv = "_URUNC_EXEC_PROCESS"
	execReadyEnv   = "_URUNC_EXEC_READY_FD"
	execParentEnv  = "_URUNC_EXEC_PARENT_FD"
)

// execReadyTimeout bounds how long the detaching parent waits for the child
// to report the session ready. It comfortably exceeds dialAgent's retry window.
// execParentTimeout bounds how long the child then waits for the parent to be
// gone before it starts the guest command; the parent exits right after the
// report, so this only matters if something holds the parent up.
const (
	execReadyTimeout  = 30 * time.Second
	execParentTimeout = 10 * time.Second
)

func execAction(cmd *cli.Command) error {
	// containerd runs exec detached: it expects the runtime to return promptly
	// with the process's pid written to --pid-file, then reaps that pid for the
	// exit status. A guest exec has no host process, so this proxy re-execs
	// itself as the detached "process": the child streams the session and exits
	// with the guest command's code, which is the code containerd reports.
	if cmd.Bool("detach") && os.Getenv(execChildEnv) == "" {
		return daemonizeExecProxy(cmd)
	}

	ready := openExecReady()
	err := runExecSession(cmd, ready)
	if err != nil {
		// Reached only when the session never opened (a successful session
		// exits the process with the guest's code). Tell the waiting parent.
		ready.fail(err)
	}
	return err
}

// runExecSession opens the guest session described by the command and pumps
// it until the guest process exits, then exits this process with its code.
func runExecSession(cmd *cli.Command, ready *execReady) error {
	unikontainer, err := getUnikontainer(cmd)
	if err != nil {
		return err
	}

	proc, err := loadExecProcess(cmd)
	if err != nil {
		return err
	}
	if len(proc.Args) == 0 {
		return fmt.Errorf("exec requires a command to run")
	}

	// The agent listens on the same vsock port for every monitor; only the host
	// side of the transport differs (qemu: host vhost-vsock; firecracker and
	// cloud-hypervisor: a host unix socket).
	transport := unikontainer.AgentTransportInfo()
	var conn io.ReadWriteCloser
	if transport.VsockUDS != "" {
		conn, err = dialAgentHybridVsock(transport.VsockUDS, agentproto.DefaultVsockPort, 15*time.Second)
		if err != nil {
			return fmt.Errorf("could not reach the in-guest exec agent over hybrid vsock %s (port %d): %w", transport.VsockUDS, agentproto.DefaultVsockPort, err)
		}
	} else {
		conn, err = dialAgent(transport.CID, agentproto.DefaultVsockPort, 15*time.Second)
		if err != nil {
			return fmt.Errorf("could not reach the in-guest exec agent at vsock(%d,%d): %w", transport.CID, agentproto.DefaultVsockPort, err)
		}
	}
	defer conn.Close()

	// Foreground exec (no --detach) records its own pid; the detached path
	// records the child's pid in daemonizeExecProxy instead.
	if os.Getenv(execChildEnv) == "" {
		if pidFile := cmd.String("pid-file"); pidFile != "" {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
		}
	}

	req := agentproto.OpenRequest{
		Argv: proc.Args,
		Env:  proc.Env,
		Cwd:  proc.Cwd,
		User: userSpec(proc.User),
		TTY:  proc.Terminal,
	}
	if proc.Terminal {
		return runTTYSession(cmd, conn, req, ready)
	}
	return runStreamSession(conn, req, ready)
}

// execReady is the detached proxy child's end of the handshake with its
// parent, over two pipes the parent created.
//
// On the readiness pipe the child writes a zero byte once the session is
// ready to open (agent reached and, for a tty, the console fd handed over),
// or the error text if it fails before that. Without it the parent would
// return success to containerd as soon as the child was spawned, and a child
// that then failed would leave containerd blocked forever waiting for a
// console that never arrives.
//
// The parent pipe is held open by the parent and never written; the child
// reads EOF from it when the parent has exited, i.e. returned to containerd.
// The child starts the guest command only then, because containerd attaches
// an exec's I/O only after the runtime's exec call returns, and its client
// drops output still in flight when the exit arrives: a quick guest command
// that ran to completion before that point would lose its output.
type execReady struct {
	ready  *os.File
	parent *os.File
}

// openExecReady picks up the handshake pipes from the environment; outside
// the detached child it is a no-op.
func openExecReady() *execReady {
	fdFile := func(env, name string) *os.File {
		fdStr := os.Getenv(env)
		if fdStr == "" {
			return nil
		}
		fd, err := strconv.Atoi(fdStr)
		if err != nil {
			return nil
		}
		return os.NewFile(uintptr(fd), name)
	}
	return &execReady{
		ready:  fdFile(execReadyEnv, "urunc-exec-ready"),
		parent: fdFile(execParentEnv, "urunc-exec-parent"),
	}
}

// ok reports that the session is ready to open, then waits for the parent to
// be gone (see execReady) so the caller can start the guest command.
func (r *execReady) ok() {
	if r.ready != nil {
		_, _ = r.ready.Write([]byte{0})
		_ = r.ready.Close()
		r.ready = nil
	}
	if r.parent == nil {
		return
	}
	gone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r.parent)
		close(gone)
	}()
	select {
	case <-gone:
	case <-time.After(execParentTimeout):
	}
	_ = r.parent.Close()
	r.parent = nil
}

// fail reports why the session could not be opened and closes the pipes.
func (r *execReady) fail(err error) {
	if r.ready != nil {
		_, _ = r.ready.WriteString(err.Error())
		_ = r.ready.Close()
		r.ready = nil
	}
	if r.parent != nil {
		_ = r.parent.Close()
		r.parent = nil
	}
}

// waitExecReady is the parent's side: it blocks until the child reports the
// session open (nil), reports a failure (its text), exits without reporting,
// or the timeout elapses.
func waitExecReady(rd *os.File, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		first := make([]byte, 1)
		n, err := rd.Read(first)
		if n == 1 && first[0] == 0 {
			done <- nil
			return
		}
		rest, _ := io.ReadAll(rd)
		text := string(first[:n]) + string(rest)
		switch {
		case text != "":
		case err != nil && err != io.EOF:
			text = err.Error()
		default:
			text = "the proxy exited before the session was open (see the runtime log)"
		}
		done <- errors.New(text)
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timed out after %s waiting for the session to open", timeout)
	}
}

// daemonizeExecProxy re-execs urunc as the detached proxy "process": the child
// (marked by execChildEnv) runs the session and exits with the guest command's
// code, while this parent waits for the child to report the session open,
// records the child's pid and returns so containerd's exec call completes.
//
// Which stdio the child gets depends on how the session is attached. A
// non-tty exec streams over the pipes containerd passed as our stdio, so the
// child inherits them. A tty exec bridges the pty it hands over the console
// socket instead, and must NOT inherit our stdio: containerd runs a detached
// tty exec with its stdout/stderr captured through a pipe that it drains to
// EOF before its exec call returns, so an inherited write end would hold that
// pipe open for the whole session, containerd would never attach the console,
// and the client would hang. The child gets /dev/null there, as runc's init
// does.
func daemonizeExecProxy(cmd *cli.Command) error {
	child := exec.Command("/proc/self/exe", os.Args[1:]...) //nolint:gosec
	child.Env = append(os.Environ(), execChildEnv+"=1")
	// Snapshot the process spec now: containerd removes its --process temp file
	// once we return, so the detached child receives it through the environment.
	if src := cmd.String("process"); src != "" {
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("could not read process spec %s: %w", src, err)
		}
		child.Env = append(child.Env, execProcessEnv+"="+string(data))
	}
	readyR, readyW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("could not create the exec readiness pipe: %w", err)
	}
	defer readyR.Close()
	parentR, parentW, err := os.Pipe()
	if err != nil {
		_ = readyW.Close()
		return fmt.Errorf("could not create the exec parent pipe: %w", err)
	}
	// Kept open (and referenced, so no finalizer closes it early) until this
	// process exits; the child sees EOF then and starts the guest command.
	defer parentW.Close()
	// ExtraFiles start at fd 3 in the child.
	child.ExtraFiles = []*os.File{readyW, parentR}
	child.Env = append(child.Env, execReadyEnv+"=3", execParentEnv+"=4")
	if cmd.String("console-socket") == "" {
		child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	}
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		_ = readyW.Close()
		_ = parentR.Close()
		return fmt.Errorf("could not start the exec proxy: %w", err)
	}
	_ = readyW.Close()
	_ = parentR.Close()
	if err := waitExecReady(readyR, execReadyTimeout); err != nil {
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
		return fmt.Errorf("exec proxy could not open the guest session: %w", err)
	}
	if pidFile := cmd.String("pid-file"); pidFile != "" {
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o644); err != nil {
			return fmt.Errorf("could not write pid file: %w", err)
		}
	}
	// Detach: leave the child running for containerd to reap by pid.
	return child.Process.Release()
}

// loadExecProcess builds the process to exec, from --process (the path
// containerd passes) or, failing that, from the CLI args and flags.
func loadExecProcess(cmd *cli.Command) (*specs.Process, error) {
	// The detached proxy child receives the spec through the environment (see
	// daemonizeExecProxy), since containerd's --process file is already gone.
	if raw := os.Getenv(execProcessEnv); raw != "" {
		var p specs.Process
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, fmt.Errorf("could not parse process spec from environment: %w", err)
		}
		return &p, nil
	}
	if path := cmd.String("process"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("could not read process spec %s: %w", path, err)
		}
		var p specs.Process
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("could not parse process spec %s: %w", path, err)
		}
		return &p, nil
	}

	p := &specs.Process{
		Args:     cmd.Args().Tail(),
		Env:      cmd.StringSlice("env"),
		Cwd:      cmd.String("cwd"),
		Terminal: cmd.Bool("tty"),
	}
	return p, nil
}

// userSpec renders an OCI user as the agent's "uid:gid" spec. An all-zero user
// means root, which the agent treats as the empty (default) spec.
func userSpec(u specs.User) string {
	if u.UID == 0 && u.GID == 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d", u.UID, u.GID)
}

// newStreamID picks the protocol stream id for this session. Over vsock each
// session is its own connection, but a random id keeps sessions independent of
// any stream left behind on a shared connection and is cheap insurance; frames
// for other streams are ignored (see the session loops).
func newStreamID() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	id := binary.BigEndian.Uint32(b[:])
	if id == 0 {
		id = 1
	}
	return id
}

// dialAgent connects to the agent socket, retrying until the monitor has
// created it (the container may still be starting) or the timeout elapses.
//
// The socket lives deep under the monitor rootfs, past the ~108-byte AF_UNIX
// sun_path limit. The container may still be starting when exec runs, so the
// dial retries until the agent's vsock listener is up or the timeout elapses.
//
// The transport is AF_VSOCK to the guest CID, the same transport hull uses.
// Linux hosts reach a guest's vsock through /dev/vhost-vsock, so no host unix
// socket is bridged. A connected vsock socket behaves as a stream, so it is
// wrapped in an *os.File and used directly as the frame conn.
func dialAgent(cid, port uint32, timeout time.Duration) (io.ReadWriteCloser, error) {
	deadline := time.Now().Add(timeout)
	for {
		fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		err = unix.Connect(fd, &unix.SockaddrVM{CID: cid, Port: port})
		if err == nil {
			return os.NewFile(uintptr(fd), "urunc-agent-vsock"), nil
		}
		_ = unix.Close(fd)
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// dialAgentHybridVsock reaches the agent over a hybrid vsock, which firecracker
// and cloud-hypervisor expose to the host as a unix socket that multiplexes
// guest ports. To open a connection to a guest port, the host connects to that
// socket and sends "CONNECT <port>"; the monitor answers "OK <hostport>" once
// the guest is accepting, after which the stream carries the session exactly as
// an AF_VSOCK connection would.
//
// The socket lives deep under the monitor rootfs, past the AF_UNIX sun_path
// limit, so it is dialed from its own directory by basename. The container may
// still be starting, so this retries until the socket exists, the monitor
// answers OK (the guest agent is listening) or the timeout elapses.
func dialAgentHybridVsock(udsPath string, guestPort uint32, timeout time.Duration) (io.ReadWriteCloser, error) {
	dir := filepath.Dir(udsPath)
	base := filepath.Base(udsPath)
	deadline := time.Now().Add(timeout)
	for {
		conn, err := dialUnixFrom(dir, base)
		if err == nil {
			if err = hybridVsockConnect(conn, guestPort); err == nil {
				return conn, nil
			}
			_ = conn.Close()
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// dialUnixFrom dials a unix socket by basename from its directory, so the path
// on the wire stays within the AF_UNIX limit. The chdir is restored before it
// returns.
func dialUnixFrom(dir, base string) (net.Conn, error) {
	prev, err := os.Getwd()
	if err != nil {
		prev = "/"
	}
	if err = os.Chdir(dir); err != nil {
		return nil, fmt.Errorf("could not chdir to agent socket dir %s: %w", dir, err)
	}
	defer func() { _ = os.Chdir(prev) }()
	return net.Dial("unix", base)
}

// hybridVsockConnect performs the host-initiated hybrid-vsock handshake (used by
// firecracker and cloud-hypervisor) for the given guest port. The reply line is
// read one byte at a time so no session bytes that follow "OK <hostport>\n" are
// swallowed by a buffer.
func hybridVsockConnect(conn net.Conn, port uint32) error {
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := conn.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				break
			}
			line = append(line, buf[0])
			if len(line) > 64 {
				return fmt.Errorf("hybrid vsock CONNECT reply too long")
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("hybrid vsock did not answer CONNECT %d: %w", port, err)
		}
	}
	if !strings.HasPrefix(string(line), "OK ") {
		return fmt.Errorf("hybrid vsock refused CONNECT %d: %q", port, strings.TrimSpace(string(line)))
	}
	return nil
}

// runStreamSession drives a non-tty exec: stdin is forwarded to the guest and
// stdout/stderr come back on separate streams. It exits the process with the
// guest command's exit code.
func runStreamSession(conn io.ReadWriteCloser, req agentproto.OpenRequest, ready *execReady) error {
	stream := newStreamID()

	// Concurrent writers (stdin pump and the signal forwarder) must not
	// interleave frames on the wire.
	var wmu sync.Mutex
	writeFrame := func(typ byte, payload []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		return agentproto.WriteFrame(conn, typ, stream, payload)
	}
	writeJSON := func(typ byte, v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return agentproto.WriteJSON(conn, typ, stream, v)
	}

	ready.ok()
	if err := writeJSON(agentproto.TypeOpen, req); err != nil {
		return err
	}

	// Without a raw-mode tty, termination signals reaching this proxy are
	// forwarded to the guest process rather than killing the proxy silently.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigs {
			if s, ok := sig.(syscall.Signal); ok {
				_ = writeJSON(agentproto.TypeSignal, agentproto.Signal{Signal: int(s)})
			}
		}
	}()

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if werr := writeFrame(agentproto.TypeStdin, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				_ = writeFrame(agentproto.TypeCloseStdin, nil)
				return
			}
		}
	}()

	// Guest bytes reaching an interactive terminal go through the filter; a
	// pipe or file (the usual non-tty destination) is left untouched.
	guestOut := guestTerminalWriter(os.Stdout)
	guestErr := guestTerminalWriter(os.Stderr)

	for {
		f, err := agentproto.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				return fmt.Errorf("exec agent closed the connection before the process exited")
			}
			return err
		}
		if f.Stream != stream {
			// Leftover of an earlier session on this peer (see newStreamID).
			continue
		}
		switch f.Type {
		case agentproto.TypeStdout:
			_, _ = guestOut.Write(f.Payload)
		case agentproto.TypeStderr:
			_, _ = guestErr.Write(f.Payload)
		case agentproto.TypeExit:
			var ex agentproto.Exit
			if err := json.Unmarshal(f.Payload, &ex); err != nil {
				return err
			}
			os.Exit(ex.Code)
		case agentproto.TypeError:
			var e agentproto.Error
			_ = json.Unmarshal(f.Payload, &e)
			return fmt.Errorf("exec failed in guest: %s", sanitizeGuestText(e.Message))
		}
	}
}
