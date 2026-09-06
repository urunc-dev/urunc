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

package unikontainers

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

type IPCMessage string

const (
	// readyPipeName is the FIFO in the container's state dir (BaseDir).
	readyPipeName = "urunc-ready.fifo"
	// readyOK is the byte the urunc monitor process writes on a successful setup.
	readyOK byte = 0
	// ReadyPipeFD is the descriptor the FIFO's write end lands on inside
	// the urunc monitor process.The ready pipe is passed as the single,
	// first ExtraFile so it is always fd 3. All other files are placed after it.
	ReadyPipeFD = 3
	// Socket for messages towards reexec. The reexec process listens in this socket
	reexecSock = "reexec.sock"
	// Socket for messages from reexec. The reexec process writes in this socket
	uruncSock                = "urunc.sock"
	ReexecStarted IPCMessage = "RX_START"
	AckReexec     IPCMessage = "UC_ACK"
	StartExecve   IPCMessage = "UC_START"
	StartSuccess  IPCMessage = "RX_SUCCESS"
	StartErr      IPCMessage = "RX_ERROR"
	maxRetries               = 50
	waitTime                 = 5 * time.Millisecond
	FromReexec               = true
)

func getSockAddr(dir string, name string) string {
	return filepath.Join(dir, name)
}

func getUruncSockAddr(containerDir string) string {
	return getSockAddr(containerDir, uruncSock)
}

func getReexecSockAddr(baseDir string) string {
	return getSockAddr(baseDir, reexecSock)
}

func getReadyPipePath(baseDir string) string {
	return filepath.Join(baseDir, readyPipeName)
}

func ensureValidSockAddr(sockAddr string) error {
	if sockAddr == "" {
		return fmt.Errorf("socket address is empty")
	}
	if len(sockAddr) > 108 {
		return fmt.Errorf("socket address \"%s\" is too long", sockAddr)
	}
	return nil
}

// sockAddrExists returns true if given sock address exists
// returns false if any error is encountered
func SockAddrExists(sockAddr string) bool {
	_, err := os.Stat(sockAddr)
	if err == nil {
		return true
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	uniklog.WithError(err).Errorf("Failed to get file info for %s", sockAddr)
	return false
}

// SendIPCMessage creates a new connection to socketAddress, sends the message and closes the connection
func SendIPCMessage(socketAddress string, message IPCMessage) error {
	conn, err := net.Dial("unix", socketAddress)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(message)); err != nil {
		return fmt.Errorf("failed to send message \"%s\" to \"%s\": %w", message, socketAddress, err)
	}
	return nil
}

// sendIPCMessageWithRetry attempts to connect to socketAddress. if successful, sends the message and closes the connection
func sendIPCMessageWithRetry(socketAddress string, message IPCMessage, mustBeValid bool) error {
	if mustBeValid {
		err := ensureValidSockAddr(socketAddress)
		if err != nil {
			return err
		}
	}
	var conn *net.UnixConn
	var err error
	retry := 0
	for {
		conn, err = net.DialUnix("unix", nil, &net.UnixAddr{Name: socketAddress, Net: "unix"})
		if err == nil {
			break
		}
		retry++
		if retry >= maxRetries {
			return fmt.Errorf("failed to connect to %s, exceeded max retries", socketAddress)
		}
		time.Sleep(waitTime)
	}
	defer func() {
		err = conn.Close()
		if err != nil {
			logrus.WithError(err).Error("failed to close connection")
		}
	}()
	_, err = conn.Write([]byte(message))
	if err != nil {
		logrus.WithError(err).Errorf("failed to send message \"%s\" to \"%s\"", message, socketAddress)
	}
	return err
}

// createListener sets up a listener for new connection to socketAddress
func createListener(socketAddress string, mustBeValid bool) (*net.UnixListener, error) {
	if mustBeValid {
		err := ensureValidSockAddr(socketAddress)
		if err != nil {
			return nil, err
		}
	}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketAddress, Net: "unix"})
	if err != nil {
		return nil, err
	}

	return listener, nil
}

// awaitMessage opens a new connection to socketAddress
// and waits for a given message
func AwaitMessage(listener *net.UnixListener, expectedMessage IPCMessage) error {
	conn, err := listener.AcceptUnix()
	if err != nil {
		return err
	}
	defer func() {
		err = conn.Close()
		if err != nil {
			logrus.WithError(err).Error("failed to close connection")
		}
	}()
	buf := make([]byte, len(expectedMessage))
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read from socket: %w", err)
	}
	msg := string(buf[0:n])
	if msg != string(expectedMessage) {
		return fmt.Errorf("received unexpected message: %s (expected %s)", msg, expectedMessage)
	}
	return nil
}

// CreateReadyPipe creates the ready FIFO in the container's state dir and
// opens its write end. The returned file is meant to be passed as the urunc's
// monitor process' single Process.ExtraFiles entry, so the monitor inherits it
// at ReadyPipeFD.
func CreateReadyPipe(baseDir string) (*os.File, error) {
	path := getReadyPipePath(baseDir)

	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to remove stale ready pipe %s: %w", path, err)
	}

	err = syscall.Mkfifo(path, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to create ready pipe %s: %w", path, err)
	}

	// Open O_RDWR, which never blocks on a FIFO and, more importantly,
	// keeps a write end open from the moment the monitor is born. That way
	// the FIFO always "has a writer" while the monitor runs, so "urunc
	// start" blocks reading it rather than seeing a premature EOF, yet it
	// still reports EOF if the monitor dies without writing.
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open ready pipe %s: %w", path, err)
	}

	return file, nil
}

// openReadReadyPipe opens the read end of the ready FIFO that "urunc create" left in
// the state dir.
func (u *Unikontainer) openReadReadyPipe() error {
	path := getReadyPipePath(u.BaseDir)

	// Open as non-blocking so the open never waits on a writer;
	// the actual read in awaitReady blocks through the Go runtime poller.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("failed to open ready pipe %s: %w", path, err)
	}
	u.readyPipe = os.NewFile(uintptr(fd), path)

	return nil
}

// closeReadReadyPipe closes the read end of the ready FIFO and removes it.
func (u *Unikontainer) closeReadReadyPipe() error {
	if u.readyPipe != nil {
		err := u.readyPipe.Close()
		if err != nil {
			uniklog.WithError(err).Error("failed to close the ready pipe")
		}
	}

	path := getReadyPipePath(u.BaseDir)
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove ready pipe %s: %w", path, err)
	}

	return nil
}

// awaitReadyPipe blocks until the urunc monitor process reports the outcome of its setup
// over the ready FIFO. A single readyOK byte means success; any other byte, a read
// error, or EOF (the monitor exited before reporting) means failure.
func (u *Unikontainer) awaitReadyPipe() error {
	buf := make([]byte, 1)

	n, err := u.readyPipe.Read(buf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("the monitor exited before reporting a successful start")
		}
		return fmt.Errorf("failed to read from the ready pipe: %w", err)
	}
	// buf is make([]byte, 1); index 0 is a valid value
	if n != 1 || buf[0] != readyOK { //nolint:gosec
		return fmt.Errorf("the monitor reported a failed start")
	}

	return nil
}

// signalReady reports the outcome of the monitor setup to "urunc start" over the
// ready FIFO inherited, then closes it so it is not left open in the
// monitor.
func signalReady(ok bool) error {
	status := byte(1)
	if ok {
		status = readyOK
	}

	_, werr := syscall.Write(ReadyPipeFD, []byte{status})
	cerr := syscall.Close(ReadyPipeFD)

	return errors.Join(werr, cerr)
}
