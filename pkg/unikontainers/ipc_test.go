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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetSockAddr(t *testing.T) {
	dir := "/tmp"
	name := "test.sock"
	expected := filepath.Join(dir, name)
	result := getSockAddr(dir, name)
	assert.Equal(t, expected, result, "Expected %s, but got %s", expected, result)
}

func TestGetUruncSockAddr(t *testing.T) {
	containerDir := "/tmp/container"
	expected := filepath.Join(containerDir, uruncSock)
	result := getUruncSockAddr(containerDir)
	assert.Equal(t, expected, result, "Expected %s, but got %s", expected, result)
}

func TestEnsureValidSockAddr(t *testing.T) {
	validSockAddr := "/tmp/valid.sock"
	emptySockAddr := ""
	longSockAddr := string(make([]byte, 109))

	assert.NoError(t, ensureValidSockAddr(validSockAddr), "Expected no error for valid socket address")
	assert.Error(t, ensureValidSockAddr(emptySockAddr), "Expected error for empty socket address")
	assert.Error(t, ensureValidSockAddr(longSockAddr), "Expected error for long socket address")
}

func TestSockAddrExists(t *testing.T) {
	existingSockAddr := "/tmp/existing.sock"
	nonExistingSockAddr := "/tmp/non_existing.sock"

	// Create a temporary socket file
	f, err := os.Create(existingSockAddr)
	if err != nil {
		t.Fatalf("Failed to create temporary socket file: %v", err)
	}
	defer os.Remove(existingSockAddr)
	f.Close()

	assert.True(t, SockAddrExists(existingSockAddr), "Expected socket address to exist")
	assert.False(t, SockAddrExists(nonExistingSockAddr), "Expected socket address to not exist")
}

func readExpectedMsgFromSocket(socketAddr string, message IPCMessage, waitChan chan<- bool) error {
	defer os.Remove(socketAddr)
	listener, err := net.Listen("unix", socketAddr)
	if err != nil {
		return err
	}
	defer listener.Close()
	waitChan <- true

	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	buf := make([]byte, len(message))
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}

	if string(message) != string(buf[:n]) {
		return fmt.Errorf("Expected %s, but got %s", message, string(buf[:n]))
	}
	return nil
}

func testSendIPCMessageHelper(t *testing.T, socketAddress string, message IPCMessage, sendFunc func(string, IPCMessage) error) {
	errChan := make(chan error, 1)
	waitChan := make(chan bool, 1)

	go func() {
		errChan <- readExpectedMsgFromSocket(socketAddress, message, waitChan)
	}()

	select {
	case <-waitChan:
		err := sendFunc(socketAddress, message)
		assert.NoError(t, err, "Expected no error in sending IPC message")
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out")
	}

	select {
	case err := <-errChan:
		if err != nil {
			t.Fatal(err.Error())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Test timed out")
	}
}

func TestSendIPCMessage(t *testing.T) {
	socketAddress := "/tmp/test.sock"
	message := ReexecStarted

	testSendIPCMessageHelper(t, socketAddress, message, SendIPCMessage)
}

func TestSendIPCMessageWithRetry(t *testing.T) {
	socketAddress := "/tmp/test_retry.sock"
	message := ReexecStarted

	// Wrapping the sendIPCMessageWithRetry function to match the expected function signature.
	sendWithRetry := func(addr string, msg IPCMessage) error {
		return sendIPCMessageWithRetry(addr, msg, true)
	}

	testSendIPCMessageHelper(t, socketAddress, message, sendWithRetry)
}

func TestCreateListener(t *testing.T) {
	socketAddress := "/tmp/test_create_listener.sock"

	listener, err := createListener(socketAddress, true)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	assert.NotNil(t, listener, "Expected listener to be created")
}

func TestAwaitMessage(t *testing.T) {
	socketAddress := "/tmp/test_await_message.sock"
	expectedMessage := ReexecStarted

	listener, err := createListener(socketAddress, true)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := net.Dial("unix", socketAddress)
		if err != nil {
			t.Errorf("Failed to dial connection: %v", err)
		}
		defer conn.Close()

		_, err = conn.Write([]byte(expectedMessage))
		if err != nil {
			t.Errorf("Failed to send message: %v", err)
		}
	}()

	err = AwaitMessage(listener, expectedMessage)
	assert.NoError(t, err, "Expected no error in awaiting message")
}
