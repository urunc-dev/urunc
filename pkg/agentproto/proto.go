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

// Package agentproto defines the wire protocol spoken between the host
// (urunc-macos exec / attach) and the in-guest urunit-agent.
//
// The protocol is a minimal multiplexed byte stream, designed to run over
// any single reliable transport: a vsock connection (Vz backend), a
// virtio-serial port (QEMU backend, where macOS has no vhost-vsock), or a
// plain unix socket in tests.
//
// Every frame is:
//
//	type    uint8
//	stream  uint32 (big endian)  - session identifier chosen by the client
//	length  uint32 (big endian)  - payload length
//	payload [length]byte
//
// Control payloads (Open, Resize, Signal, Exit, Error) are JSON; stdio
// payloads are raw bytes.
package agentproto

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Frame types.
const (
	TypeOpen       byte = 1 // client -> agent: OpenRequest (JSON)
	TypeStdin      byte = 2 // client -> agent: raw stdin bytes
	TypeStdout     byte = 3 // agent -> client: raw stdout bytes
	TypeStderr     byte = 4 // agent -> client: raw stderr bytes (non-tty only)
	TypeCloseStdin byte = 5 // client -> agent: EOF on stdin
	TypeResize     byte = 6 // client -> agent: Resize (JSON)
	TypeSignal     byte = 7 // client -> agent: Signal (JSON)
	TypeExit       byte = 8 // agent -> client: Exit (JSON), closes the stream
	TypeError      byte = 9 // agent -> client: Error (JSON), closes the stream
)

// DefaultVsockPort is the guest vsock port the agent listens on (Vz).
const DefaultVsockPort = 1024

// VirtioPortName is the virtio-serial port name the agent uses on QEMU.
// The guest device appears as /dev/virtio-ports/<name>.
const VirtioPortName = "io.urunc.agent.0"

// MaxPayload bounds a single frame payload.
const MaxPayload = 1 << 20

// OpenRequest starts a new process in the guest on the given stream.
type OpenRequest struct {
	Argv []string `json:"argv"`
	Env  []string `json:"env,omitempty"`
	Cwd  string   `json:"cwd,omitempty"`
	// User selects the identity to run as: a name ("claude"), a uid
	// ("501") or "uid:gid" ("501:1000"). Empty means root. The agent
	// resolves names and lone uids against the guest's /etc/passwd and
	// fills in HOME/USER/LOGNAME/SHELL and supplementary groups.
	User string `json:"user,omitempty"`
	TTY  bool   `json:"tty"`
	Rows uint16 `json:"rows,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
}

// Resize updates the window size of a tty session.
type Resize struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// Signal delivers a signal to the session's process.
type Signal struct {
	Signal int `json:"signal"`
}

// Exit reports the final status of a session.
type Exit struct {
	Code int `json:"code"`
}

// Error reports a session setup or runtime failure.
type Error struct {
	Message string `json:"message"`
}

// Frame is a single decoded protocol frame.
type Frame struct {
	Type    byte
	Stream  uint32
	Payload []byte
}

// ReadFrame reads one frame from r.
func ReadFrame(r io.Reader) (Frame, error) {
	var hdr [9]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, err
	}
	f := Frame{
		Type:   hdr[0],
		Stream: binary.BigEndian.Uint32(hdr[1:5]),
	}
	length := binary.BigEndian.Uint32(hdr[5:9])
	if length > MaxPayload {
		return Frame{}, fmt.Errorf("frame payload too large: %d (header % x)", length, hdr[:])
	}
	if length > 0 {
		f.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return Frame{}, err
		}
	}
	return f, nil
}

// WriteFrame writes one frame to w. Callers must serialize concurrent
// writes themselves.
func WriteFrame(w io.Writer, typ byte, stream uint32, payload []byte) error {
	if len(payload) > MaxPayload {
		return fmt.Errorf("frame payload too large: %d", len(payload))
	}
	var hdr [9]byte
	hdr[0] = typ
	binary.BigEndian.PutUint32(hdr[1:5], stream)
	binary.BigEndian.PutUint32(hdr[5:9], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// WriteJSON marshals v and writes it as a frame of the given type.
func WriteJSON(w io.Writer, typ byte, stream uint32, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFrame(w, typ, stream, payload)
}
