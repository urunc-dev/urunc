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
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"

	"github.com/urunc-dev/urunc/pkg/agentproto"
)

// runTTYSession drives a tty exec. The real terminal lives in the guest (the
// agent allocates a pty next to the workload); this side only needs to give
// the caller a pty master to attach to and pump raw bytes between it and the
// guest session.
//
// With --console-socket (how containerd runs `nerdctl exec -it`) we open a
// host pty, hand the master to the caller over that socket and bridge the raw
// slave, which is a transparent conduit -- the guest pty does the terminal
// processing. Without it we bridge our own stdio.
func runTTYSession(cmd *cli.Command, conn io.ReadWriteCloser, req agentproto.OpenRequest, ready *execReady) error {
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

	var in io.Reader
	var out io.Writer
	var cleanup func()
	// sizeFd is the fd whose window size tracks the caller's terminal, polled
	// to forward resizes (see the resize goroutine below).
	var sizeFd uintptr

	if sockPath := cmd.String("console-socket"); sockPath != "" {
		ptmx, tty, err := pty.Open()
		if err != nil {
			return fmt.Errorf("could not allocate host pty: %w", err)
		}
		if err := sendConsoleFd(sockPath, ptmx, tty.Name()); err != nil {
			_ = ptmx.Close()
			_ = tty.Close()
			return fmt.Errorf("could not hand the console to the caller: %w", err)
		}
		// The caller owns the master now; we bridge the raw slave.
		_ = ptmx.Close()
		makeRaw(tty.Fd())
		req.Rows, req.Cols = ttySize(tty.Fd())
		sizeFd = tty.Fd()
		in, out, cleanup = tty, tty, func() { _ = tty.Close() }
	} else {
		if restore, err := makeRawRestore(os.Stdin.Fd()); err == nil {
			cleanup = restore
		} else {
			cleanup = func() {}
		}
		req.Rows, req.Cols = ttySize(os.Stdin.Fd())
		sizeFd = os.Stdin.Fd()
		in, out = os.Stdin, os.Stdout
	}
	defer cleanup()
	// done ends the resize poller when the session's read loop returns.
	done := make(chan struct{})
	defer close(done)

	// The console (if any) is handed over and the agent reached: tell the
	// parent, and start the guest command only once it has returned.
	ready.ok()
	if err := writeJSON(agentproto.TypeOpen, req); err != nil {
		return err
	}

	// A tty carries interrupts as bytes, so a signal reaching this proxy means
	// the host side is taking the session down (a kill, a closing shim). Do
	// not orphan the guest process: hang it up as a vanishing terminal would,
	// and kill it if it ignores that. Its exit then ends the session normally.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigs
		_ = writeJSON(agentproto.TypeSignal, agentproto.Signal{Signal: int(syscall.SIGHUP)})
		time.Sleep(2 * time.Second)
		_ = writeJSON(agentproto.TypeSignal, agentproto.Signal{Signal: int(syscall.SIGKILL)})
	}()

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				if werr := writeFrame(agentproto.TypeStdin, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Forward terminal resizes to the guest pty. containerd holds the pty
	// master and sets its window size when the client resizes, but it sends
	// this proxy no SIGWINCH (the proxy does not own the controlling
	// terminal), so the change is observed by polling the size fd -- the pty
	// slave, whose winsize mirrors the master. The initial size went out with
	// Open; only later changes are sent here.
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		rows, cols := req.Rows, req.Cols
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				nr, nc := ttySize(sizeFd)
				if nr == rows && nc == cols {
					continue
				}
				rows, cols = nr, nc
				if err := writeJSON(agentproto.TypeResize, agentproto.Resize{Rows: rows, Cols: cols}); err != nil {
					return
				}
			}
		}
	}()

	// A tty merges stdout and stderr onto the one terminal, and that terminal is
	// the caller's by definition, so guest output goes through the escape filter
	// (unless it is switched off).
	guestOut := out
	if !terminalFilterDisabled() {
		guestOut = &terminalSanitizer{w: out}
	}

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
		case agentproto.TypeStdout, agentproto.TypeStderr:
			_, _ = guestOut.Write(f.Payload)
		case agentproto.TypeExit:
			cleanup()
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

// sendConsoleFd hands the pty master to the caller over the runc-style console
// socket: a SCM_RIGHTS message carrying the fd, with the pty *slave* path as
// the payload, exactly as runc does (containerd keys the console it copies
// stdin/stdout through on that path).
func sendConsoleFd(sockPath string, master *os.File, ptsPath string) error {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("console socket is not a unix socket")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return err
	}
	oob := unix.UnixRights(int(master.Fd()))
	var sendErr error
	if cerr := raw.Control(func(fd uintptr) {
		sendErr = unix.Sendmsg(int(fd), []byte(ptsPath), oob, nil, 0)
	}); cerr != nil {
		return cerr
	}
	return sendErr
}

// makeRaw puts a tty fd into raw mode without keeping a restore closure (used
// for a pty slave we own and simply close afterwards).
func makeRaw(fd uintptr) {
	_, _ = makeRawRestore(fd)
}

// makeRawRestore puts a tty fd into raw mode and returns a closure restoring
// the previous settings.
func makeRawRestore(fd uintptr) (func(), error) {
	old, err := unix.IoctlGetTermios(int(fd), unix.TCGETS)
	if err != nil {
		return nil, err
	}
	raw := *old
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(int(fd), unix.TCSETS, &raw); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(int(fd), unix.TCSETS, old) }, nil
}

// ttySize returns the window size of a tty fd, defaulting to 24x80.
func ttySize(fd uintptr) (rows, cols uint16) {
	ws, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	if err != nil || ws.Row == 0 || ws.Col == 0 {
		return 24, 80
	}
	return ws.Row, ws.Col
}
