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
	"time"

	"github.com/sirupsen/logrus"
)

type IPCMessage string

const (
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
	var conn net.Conn
	var err error

	// FIX #405: Backoff retry loop to handle IPC socket race conditions during reexec
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("unix", socketAddress, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err != nil {
		return fmt.Errorf("timeout waiting for ipc socket %s: %w", socketAddress, err)
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

// AwaitMessage accepts a connection from the listener and waits for the 
// expected IPC message. It implements a 10-second timeout to prevent 
// the process from blocking indefinitely (Fixes #405)
func AwaitMessage(listener *net.UnixListener, expectedMessage IPCMessage) error {
	timeout := 10 * time.Second
	deadline := time.Now().Add(timeout)

	// Set deadline for the initial connection (Accept)
	if err := listener.SetDeadline(deadline); err != nil {
		return fmt.Errorf("failed to set listener deadline: %w", err)
	}

	conn, err := listener.AcceptUnix()
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return fmt.Errorf("IPC handshake timeout: no connection received within %v", timeout)
		}
		return fmt.Errorf("failed to accept IPC connection: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logrus.WithError(cerr).Error("failed to close IPC connection")
		}
	}()

	// Set deadline for the actual data transfer (Read)
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("failed to set connection deadline: %w", err)
	}

	// io.ReadFull ensures we don't return early with a partial message
	buf := make([]byte, len(expectedMessage))
	if _, err := io.ReadFull(conn, buf); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("connection closed before full message was received")
		}
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return fmt.Errorf("IPC handshake timeout: message not received within %v", timeout)
		}
		return fmt.Errorf("failed to read from IPC socket: %w", err)
	}

	if string(buf) != string(expectedMessage) {
		return fmt.Errorf("received unexpected message: %q (expected %q)", string(buf), expectedMessage)
	}

	return nil
}
