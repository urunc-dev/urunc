//go:build linux

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
	"strconv"
	"time"

	"golang.org/x/sys/unix"

	"github.com/urunc-dev/urunc/pkg/agentproto"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
)

const (
	// guestSignalBinary is the urunit the guest init staged in the root.
	// URUNIT_MODE=signal makes it send argv[1] to PID 1.
	guestSignalBinary = "/run/urunc/urunit"
	// guestSignalTimeout bounds the whole exchange, so that a guest that
	// does not answer leaves time for the monitor to be signalled instead.
	guestSignalTimeout = 5 * time.Second
	guestSignalStream  = 1
)

// guestSignaller returns the GuestSignaller of u: it hands a signal to the
// guest's init through the exec agent.
func guestSignaller(u *unikontainers.Unikontainer) unikontainers.GuestSignaller {
	return func(sig unix.Signal) error {
		conn, err := dialAgentTransport(u.AgentTransportInfo(), 2*time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()
		return signalGuestInit(conn, sig, guestSignalTimeout)
	}
}

// signalGuestInit runs the guest's urunit in its signal role over conn, an
// open agent connection, and waits for it to exit. It returns nil when the
// signal reached PID 1.
func signalGuestInit(conn io.ReadWriteCloser, sig unix.Signal, timeout time.Duration) error {
	req := agentproto.OpenRequest{
		Argv: []string{guestSignalBinary, strconv.Itoa(int(sig))},
		Env:  []string{"URUNIT_MODE=signal"},
	}
	if err := agentproto.WriteJSON(conn, agentproto.TypeOpen, guestSignalStream, req); err != nil {
		return fmt.Errorf("open the signal session: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		for {
			f, err := agentproto.ReadFrame(conn)
			if err != nil {
				done <- fmt.Errorf("%w: %w", unikontainers.ErrGuestSignalUnconfirmed, err)
				return
			}
			switch f.Type {
			case agentproto.TypeExit:
				var ex agentproto.Exit
				if err := json.Unmarshal(f.Payload, &ex); err != nil {
					done <- fmt.Errorf("decode the exit: %w", err)
					return
				}
				if ex.Code != 0 {
					done <- fmt.Errorf("%s exited with %d", guestSignalBinary, ex.Code)
					return
				}
				done <- nil
				return
			case agentproto.TypeError:
				var e agentproto.Error
				_ = json.Unmarshal(f.Payload, &e)
				done <- fmt.Errorf("the agent refused the signal session: %s", e.Message)
				return
			}
		}
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = conn.Close()
		return fmt.Errorf("no answer from the guest within %s", timeout)
	}
}
