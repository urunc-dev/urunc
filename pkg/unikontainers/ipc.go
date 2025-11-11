// Copyright (c) 2023-2025, Nubificus LTD
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
	startSock                   = "start.sock"
	uruncSock                   = "urunc.sock"
	ReexecStarted    IPCMessage = "BOOTED"
	AckReexec        IPCMessage = "ACK"
	StartExecve      IPCMessage = "START"
	ContainerStarted IPCMessage = "CNTR_STARTED"
	maxRetries                  = 50
	waitTime                    = 5 * time.Millisecond
)

func getSockAddr(dir string, name string) string {
	return filepath.Join(dir, name)
}

func getUruncSockAddr(containerDir string) string {
	return getSockAddr(containerDir, uruncSock)
}

func getStartSockAddr(baseDir string) string {
	return getSockAddr(baseDir, startSock)
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

// sockAddrExists returns true if if given sock address exists
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

// CreateListener sets up a listener for new connection to socketAddress
func CreateListener(socketAddress string, mustBeValid bool) (*net.UnixListener, func(), error) {
	if mustBeValid {
		err := ensureValidSockAddr(socketAddress)
		if err != nil {
			return nil, nil, err
		}
	}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketAddress, Net: "unix"})
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		derr := listener.Close()
		if derr != nil {
			logrus.WithError(derr).Error("failed to close listener")
		}
		derr = syscall.Unlink(socketAddress)
		if derr != nil {
			logrus.WithError(derr).Errorf("failed to unlink %s", socketAddress)
		}
	}

	return listener, cleanup, nil
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
		return fmt.Errorf("received unexpected message: %s", msg)
	}
	return nil
}
