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

// urunit-agent is a small in-guest agent that spawns additional processes
// (exec sessions) on behalf of the host. It speaks the agentproto framed
// protocol over one of two transports, tried in order:
//
//  1. A virtio-serial port (/dev/virtio-ports/io.urunc.agent.0) - used by
//     the QEMU backend on macOS, where the host has no vhost-vsock.
//  2. A vsock listener on port 1024 - used by the Vz backend, where
//     vz-runner bridges a host unix socket to guest vsock connections.
//
// For tty sessions the agent allocates a pseudo-terminal next to the
// workload, so the exec'd process gets full terminal semantics (raw mode,
// job control, SIGWINCH via resize frames).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/urunc-dev/urunc/pkg/agentproto"
	"golang.org/x/sys/unix"
)

func main() {
	log.SetPrefix("urunit-agent: ")
	log.SetFlags(0)

	// The agent starts early during guest boot with a near-empty
	// environment. exec.Command resolves bare command names against the
	// agent's own PATH, so give it a sane default.
	if os.Getenv("PATH") == "" {
		_ = os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}

	// The agent may start before init mounts the special filesystems;
	// make sure sysfs is there for transport discovery.
	if !exists("/sys/class") {
		if err := unix.Mount("sysfs", "/sys", "sysfs", 0, ""); err != nil {
			log.Printf("mount /sys: %v", err)
		}
	}

	// Poll for the virtio-serial port first (QEMU); if it never shows up,
	// fall back to vsock (Vz). Keep retrying rather than dying: the agent
	// is started in the background early during guest boot.
	for {
		for i := 0; i < 25; i++ {
			if dev := findVirtioPort(); dev != "" {
				serveVirtioPort(dev)
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		if err := serveVsock(); err != nil {
			log.Printf("vsock transport unavailable: %v; retrying discovery", err)
		}
	}
}

// findVirtioPort locates the agent's virtio-serial port device. The
// /dev/virtio-ports/<name> symlink is created by udev, which minimal
// guests do not run, so resolve the name through sysfs instead
// (/sys/class/virtio-ports/vportNpM/name) and fall back to the symlink.
func findVirtioPort() string {
	if link := "/dev/virtio-ports/" + agentproto.VirtioPortName; exists(link) {
		return link
	}
	entries, err := os.ReadDir("/sys/class/virtio-ports")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name, err := os.ReadFile("/sys/class/virtio-ports/" + e.Name() + "/name")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(name)) == agentproto.VirtioPortName {
			if dev := "/dev/" + e.Name(); exists(dev) {
				return dev
			}
		}
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// serveVirtioPort serves sessions over the virtio-serial port. The port is
// a single byte stream shared with one host peer at a time. While no host
// peer is connected, guest-side reads return EOF; treat that as "not
// connected yet" and keep the same descriptor instead of tearing down,
// otherwise frames sent right after a host connect can fall into a
// close/reopen gap.
func serveVirtioPort(dev string) {
	log.Printf("serving on virtio port %s", dev)
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		log.Fatalf("open %s: %v", dev, err)
	}
	serveConn(struct {
		io.Reader
		io.Writer
	}{patientReader{f}, f})
}

// patientReader retries reads that report EOF, which on a virtio-serial
// port only means the host peer is not currently connected.
type patientReader struct {
	f *os.File
}

func (p patientReader) Read(b []byte) (int, error) {
	for {
		n, err := p.f.Read(b)
		if n > 0 {
			return n, nil
		}
		if err == nil || err == io.EOF {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return 0, err
	}
}

// serveVsock listens on the agent vsock port and serves each connection
// concurrently.
func serveVsock() error {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("vsock socket: %w", err)
	}
	sa := &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: agentproto.DefaultVsockPort}
	if err := unix.Bind(fd, sa); err != nil {
		unix.Close(fd)
		return fmt.Errorf("vsock bind: %w", err)
	}
	if err := unix.Listen(fd, 8); err != nil {
		unix.Close(fd)
		return fmt.Errorf("vsock listen: %w", err)
	}
	log.Printf("listening on vsock port %d", agentproto.DefaultVsockPort)
	for {
		nfd, _, err := unix.Accept(fd)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return fmt.Errorf("vsock accept: %w", err)
		}
		conn := os.NewFile(uintptr(nfd), "vsock-conn")
		go func() {
			serveConn(conn)
			conn.Close()
		}()
	}
}

// connState is one protocol peer: a frame writer shared by all sessions
// multiplexed on the connection.
type connState struct {
	mu sync.Mutex
	w  io.Writer

	sessMu   sync.Mutex
	sessions map[uint32]*session
}

func (c *connState) writeFrame(typ byte, stream uint32, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return agentproto.WriteFrame(c.w, typ, stream, payload)
}

func (c *connState) writeJSON(typ byte, stream uint32, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(typ, stream, payload)
}

func (c *connState) session(id uint32) *session {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	return c.sessions[id]
}

func (c *connState) addSession(id uint32, s *session) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	c.sessions[id] = s
}

func (c *connState) removeSession(id uint32) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	delete(c.sessions, id)
}

// session is one running exec'd process.
type session struct {
	cmd   *exec.Cmd
	ptmx  *os.File       // tty sessions
	stdin io.WriteCloser // non-tty sessions
}

func serveConn(rw io.ReadWriter) {
	c := &connState{w: rw, sessions: make(map[uint32]*session)}
	for {
		f, err := agentproto.ReadFrame(rw)
		if err != nil {
			if err != io.EOF {
				log.Printf("read: %v", err)
			}
			// Tear down every session still attached to this peer.
			c.sessMu.Lock()
			for _, s := range c.sessions {
				if s.cmd.Process != nil {
					_ = s.cmd.Process.Kill()
				}
			}
			c.sessMu.Unlock()
			return
		}
		switch f.Type {
		case agentproto.TypeOpen:
			var req agentproto.OpenRequest
			if err := json.Unmarshal(f.Payload, &req); err != nil {
				_ = c.writeJSON(agentproto.TypeError, f.Stream, agentproto.Error{Message: "bad open request: " + err.Error()})
				continue
			}
			if err := c.open(f.Stream, req); err != nil {
				_ = c.writeJSON(agentproto.TypeError, f.Stream, agentproto.Error{Message: err.Error()})
			}
		case agentproto.TypeStdin:
			if s := c.session(f.Stream); s != nil {
				if s.ptmx != nil {
					_, _ = s.ptmx.Write(f.Payload)
				} else if s.stdin != nil {
					_, _ = s.stdin.Write(f.Payload)
				}
			}
		case agentproto.TypeCloseStdin:
			if s := c.session(f.Stream); s != nil && s.stdin != nil {
				_ = s.stdin.Close()
			}
		case agentproto.TypeResize:
			var rs agentproto.Resize
			if err := json.Unmarshal(f.Payload, &rs); err == nil {
				if s := c.session(f.Stream); s != nil && s.ptmx != nil {
					_ = pty.Setsize(s.ptmx, &pty.Winsize{Rows: rs.Rows, Cols: rs.Cols})
				}
			}
		case agentproto.TypeSignal:
			var sg agentproto.Signal
			if err := json.Unmarshal(f.Payload, &sg); err == nil {
				if s := c.session(f.Stream); s != nil && s.cmd.Process != nil {
					_ = s.cmd.Process.Signal(syscall.Signal(sg.Signal))
				}
			}
		default:
			log.Printf("unknown frame type %d", f.Type)
		}
	}
}

// guestUser is a resolved guest identity for an exec session.
type guestUser struct {
	uid    uint32
	gid    uint32
	groups []uint32
	name   string
	home   string
	shell  string
}

// resolveUser turns an OpenRequest user spec (name, uid, or uid:gid) into
// a guest identity. An empty spec means root and returns nil.
func resolveUser(spec string) (*guestUser, error) {
	if spec == "" {
		return nil, nil
	}

	uidPart, gidPart, hasGID := strings.Cut(spec, ":")

	var u *user.User
	var err error
	if _, nerr := strconv.ParseUint(uidPart, 10, 32); nerr == nil {
		u, err = user.LookupId(uidPart)
	} else {
		u, err = user.Lookup(uidPart)
	}

	gu := &guestUser{home: "/", shell: "/bin/sh"}
	if err == nil {
		uid, uerr := strconv.ParseUint(u.Uid, 10, 32)
		gid, gerr := strconv.ParseUint(u.Gid, 10, 32)
		if uerr != nil || gerr != nil {
			return nil, fmt.Errorf("non-numeric uid/gid for user %q", spec)
		}
		gu.uid, gu.gid = uint32(uid), uint32(gid)
		gu.name = u.Username
		if u.HomeDir != "" {
			gu.home = u.HomeDir
		}
		if sh := lookupShell(u.Username); sh != "" {
			gu.shell = sh
		}
		if ids, gerr := u.GroupIds(); gerr == nil {
			for _, id := range ids {
				if g, perr := strconv.ParseUint(id, 10, 32); perr == nil {
					gu.groups = append(gu.groups, uint32(g))
				}
			}
		}
	} else {
		// Not in /etc/passwd: accept a raw numeric uid.
		uid, nerr := strconv.ParseUint(uidPart, 10, 32)
		if nerr != nil {
			return nil, fmt.Errorf("unknown user %q", uidPart)
		}
		gu.uid = uint32(uid)
		gu.gid = uint32(uid)
		gu.name = uidPart
	}

	if hasGID {
		gid, nerr := strconv.ParseUint(gidPart, 10, 32)
		if nerr != nil {
			g, gerr := user.LookupGroup(gidPart)
			if gerr != nil {
				return nil, fmt.Errorf("unknown group %q", gidPart)
			}
			gid, nerr = strconv.ParseUint(g.Gid, 10, 32)
			if nerr != nil {
				return nil, fmt.Errorf("non-numeric gid for group %q", gidPart)
			}
		}
		gu.gid = uint32(gid)
	}

	return gu, nil
}

// lookupShell returns the login shell of a user from /etc/passwd, which
// os/user does not expose.
func lookupShell(username string) string {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[0] == username {
			return fields[6]
		}
	}
	return ""
}

// open starts the requested process on the given stream.
func (c *connState) open(stream uint32, req agentproto.OpenRequest) error {
	if len(req.Argv) == 0 {
		return fmt.Errorf("empty argv")
	}
	if c.session(stream) != nil {
		return fmt.Errorf("stream %d already in use", stream)
	}

	gu, err := resolveUser(req.User)
	if err != nil {
		return err
	}

	log.Printf("open stream %d: argv=%v tty=%v user=%q", stream, req.Argv, req.TTY, req.User)
	cmd := exec.Command(req.Argv[0], req.Argv[1:]...)
	cmd.Env = defaultEnv(req.Env, gu)
	cmd.Dir = req.Cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	if gu != nil {
		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid:    gu.uid,
			Gid:    gu.gid,
			Groups: gu.groups,
		}
		if cmd.Dir == "" {
			cmd.Dir = gu.home
		}
	}
	if cmd.Dir == "" {
		cmd.Dir = "/"
	}

	s := &session{cmd: cmd}
	if req.TTY {
		ws := &pty.Winsize{Rows: req.Rows, Cols: req.Cols}
		if ws.Rows == 0 || ws.Cols == 0 {
			ws.Rows, ws.Cols = 24, 80
		}
		// Open the pair ourselves (rather than pty.StartWithSize) so the
		// slave can be handed to the dropped-privilege child: chown it to
		// the target user, like login(1), so programs that reopen their
		// own /dev/pts/N keep working.
		ptmx, tts, err := pty.Open()
		if err != nil {
			return fmt.Errorf("open pty: %w", err)
		}
		if gu != nil {
			if cherr := os.Chown(tts.Name(), int(gu.uid), int(gu.gid)); cherr != nil {
				log.Printf("chown %s: %v", tts.Name(), cherr)
			}
		}
		_ = pty.Setsize(ptmx, ws)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = tts, tts, tts
		cmd.SysProcAttr.Setsid = true
		cmd.SysProcAttr.Setctty = true
		cmd.SysProcAttr.Ctty = 0
		if err := cmd.Start(); err != nil {
			_ = ptmx.Close()
			_ = tts.Close()
			return fmt.Errorf("start (tty): %w", err)
		}
		_ = tts.Close()
		s.ptmx = ptmx
		go func() {
			buf := make([]byte, 32*1024)
			for {
				n, err := ptmx.Read(buf)
				if n > 0 {
					if werr := c.writeFrame(agentproto.TypeStdout, stream, buf[:n]); werr != nil {
						break
					}
				}
				if err != nil {
					break
				}
			}
		}()
	} else {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start: %w", err)
		}
		s.stdin = stdin
		relay := func(r io.Reader, typ byte) {
			buf := make([]byte, 32*1024)
			for {
				n, err := r.Read(buf)
				if n > 0 {
					if werr := c.writeFrame(typ, stream, buf[:n]); werr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}
		go relay(stdout, agentproto.TypeStdout)
		go relay(stderr, agentproto.TypeStderr)
	}

	c.addSession(stream, s)
	go func() {
		code := 0
		if err := cmd.Wait(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
				if code < 0 {
					// Killed by signal: report 128+sig like a shell.
					if st, ok := ee.Sys().(syscall.WaitStatus); ok && st.Signaled() {
						code = 128 + int(st.Signal())
					}
				}
			} else {
				code = 126
			}
		}
		if s.ptmx != nil {
			_ = s.ptmx.Close()
		}
		c.removeSession(stream)
		_ = c.writeJSON(agentproto.TypeExit, stream, agentproto.Exit{Code: code})
	}()

	return nil
}

// defaultEnv fills in PATH, TERM and — when running as a resolved user —
// the login-style identity variables, unless the request carries them.
func defaultEnv(env []string, gu *guestUser) []string {
	has := func(key string) bool {
		for _, e := range env {
			if strings.HasPrefix(e, key+"=") {
				return true
			}
		}
		return false
	}
	if !has("PATH") {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	if !has("TERM") {
		env = append(env, "TERM=xterm-256color")
	}
	if gu != nil {
		if !has("HOME") {
			env = append(env, "HOME="+gu.home)
		}
		if !has("USER") {
			env = append(env, "USER="+gu.name)
		}
		if !has("LOGNAME") {
			env = append(env, "LOGNAME="+gu.name)
		}
		if !has("SHELL") {
			env = append(env, "SHELL="+gu.shell)
		}
	}
	return env
}
