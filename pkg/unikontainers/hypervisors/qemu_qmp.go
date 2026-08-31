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

package hypervisors

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// qmpDeadline bounds the whole QMP exchange so a stuck monitor cannot block
// the kill path indefinitely.
const qmpDeadline = 5 * time.Second

func (q *Qemu) SupportsGuestShutdown() bool {
	return true
}

// RequestGuestShutdown asks the guest to power down over QEMU's QMP socket.
// socketPath is already resolved and host-reachable, so it is dialed directly.
func (q *Qemu) RequestGuestShutdown(socketPath string) error {
	// One absolute deadline for dial and exchange together, so the two do not
	// stack into twice the budget.
	deadline := time.Now().Add(qmpDeadline)

	conn, err := net.DialTimeout("unix", socketPath, time.Until(deadline))
	if err != nil {
		return fmt.Errorf("%w %q: %w", ErrShutdownConnect, socketPath, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("failed to set QMP socket deadline: %w", err)
	}

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	// QEMU sends the QMP greeting unprompted right after connect.
	var greeting map[string]json.RawMessage
	if err := dec.Decode(&greeting); err != nil {
		return fmt.Errorf("%w: %w", ErrShutdownGreeting, err)
	}

	// Capabilities negotiation is mandatory before any other command.
	if err := qmpCommand(enc, dec, "qmp_capabilities", ErrShutdownHandshake); err != nil {
		return err
	}

	return qmpCommand(enc, dec, "system_powerdown", ErrShutdownCommand)
}

// qmpCommand sends an argument-less QMP command and waits for its return.
// stage names the step, so a failure is wrapped with the right sentinel.
func qmpCommand(enc *json.Encoder, dec *json.Decoder, command string, stage error) error {
	if err := enc.Encode(map[string]string{"execute": command}); err != nil {
		return fmt.Errorf("%w: failed to send QMP command %q: %w", stage, command, err)
	}
	return qmpReadReturn(dec, command, stage)
}

// qmpReadReturn reads until the command's own "return", skipping async events.
// A read failure means the monitor never answered, so it is wrapped with
// stage; an "error" object means it answered and refused, so that is Refused.
func qmpReadReturn(dec *json.Decoder, command string, stage error) error {
	for {
		var msg map[string]json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			return fmt.Errorf("%w: failed to read QMP response for %q: %w", stage, command, err)
		}
		if errObj, ok := msg["error"]; ok {
			return fmt.Errorf("%w: QMP command %q failed: %s", ErrShutdownRefused, command, string(errObj))
		}
		if _, ok := msg["return"]; ok {
			return nil
		}
	}
}
