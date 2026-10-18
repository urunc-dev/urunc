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

// Package qmp implements the QEMU Machine Protocol (QMP) client
// QMP is a JSON-RPC protocol for communicating with QEMU instances
package qmp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/sirupsen/logrus"
)

var qmplog = logrus.WithField("subsystem", "qmp")

// Client represents a connection to QEMU via QMP
type Client struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	id     int
}

// QMPGreeting is the initial message sent by QEMU
type QMPGreeting struct {
	QMP struct {
		Version struct {
			Qemu struct {
				Major int `json:"major"`
				Minor int `json:"minor"`
				Micro int `json:"micro"`
			} `json:"qemu"`
			Package string `json:"package"`
		} `json:"version"`
		Capabilities []string `json:"capabilities"`
	} `json:"QMP"`
}

// QMPRequest is a JSON-RPC request to QEMU
type QMPRequest struct {
	Execute   string                 `json:"execute"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	ID        int                    `json:"id,omitempty"`
}

// QMPResponse is a JSON-RPC response from QEMU
type QMPResponse struct {
	Return interface{} `json:"return,omitempty"`
	Error  interface{} `json:"error,omitempty"`
	Event  string      `json:"event,omitempty"`
	Data   interface{} `json:"data,omitempty"`
	ID     int         `json:"id,omitempty"`
}

// Dial connects to a QEMU instance via QMP socket
func Dial(socketPath string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to QMP socket: %w", err)
	}

	client := &Client{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
		id:     0,
	}

	// Read the greeting from QEMU
	var greeting QMPGreeting
	if err := client.readJSON(&greeting); err != nil {
		client.conn.Close()
		return nil, fmt.Errorf("failed to read QMP greeting: %w", err)
	}

	qmplog.Debugf("Connected to QEMU %d.%d.%d",
		greeting.QMP.Version.Qemu.Major,
		greeting.QMP.Version.Qemu.Minor,
		greeting.QMP.Version.Qemu.Micro)

	// Send qmp_capabilities to enable QMP
	if err := client.EnableCapabilities(); err != nil {
		client.conn.Close()
		return nil, fmt.Errorf("failed to enable QMP capabilities: %w", err)
	}

	return client, nil
}

// EnableCapabilities enables QMP capabilities
func (c *Client) EnableCapabilities() error {
	resp, err := c.Execute("qmp_capabilities", nil)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("qmp_capabilities failed: %v", resp.Error)
	}
	qmplog.Debug("QMP capabilities enabled")
	return nil
}

// SystemPowerdown sends a graceful power down request to QEMU
func (c *Client) SystemPowerdown() error {
	qmplog.Debug("Sending ACPI power down command to QEMU")
	resp, err := c.Execute("system_powerdown", nil)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("system_powerdown failed: %v", resp.Error)
	}
	return nil
}

// QueryStatus queries the status of the QEMU instance
func (c *Client) QueryStatus() (interface{}, error) {
	resp, err := c.Execute("query-status", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("query-status failed: %v", resp.Error)
	}
	return resp.Return, nil
}

// Execute sends a command to QEMU and waits for the response
func (c *Client) Execute(command string, args map[string]interface{}) (*QMPResponse, error) {
	c.id++
	req := QMPRequest{
		Execute:   command,
		Arguments: args,
		ID:        c.id,
	}

	// Send the request
	if err := c.writeJSON(req); err != nil {
		return nil, fmt.Errorf("failed to send QMP command: %w", err)
	}

	// Read responses until we get the one with matching ID
	for {
		var resp QMPResponse
		if err := c.readJSON(&resp); err != nil {
			return nil, fmt.Errorf("failed to read QMP response: %w", err)
		}

		// Handle events (may come while waiting for response)
		if resp.Event != "" {
			qmplog.Debugf("QMP event: %s - %v", resp.Event, resp.Data)
			continue
		}

		// Check if this is our response
		if resp.ID == c.id {
			return &resp, nil
		}

		// Otherwise, buffer this response and continue
		qmplog.Warnf("Received response with unexpected ID: %d (expected %d)", resp.ID, c.id)
	}
}

// writeJSON writes a JSON object to the socket
func (c *Client) writeJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	if _, err := c.writer.Write(data); err != nil {
		return err
	}

	if _, err := c.writer.WriteString("\n"); err != nil {
		return err
	}

	return c.writer.Flush()
}

// readJSON reads a JSON object from the socket
func (c *Client) readJSON(v interface{}) error {
	data, err := c.reader.ReadBytes('\n')
	if err != nil {
		return err
	}

	return json.Unmarshal(data, v)
}

// Close closes the QMP connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
