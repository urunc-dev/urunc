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
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/urunc-dev/urunc/pkg/agentproto"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
)

// closeSession is a frame type no agent sends. fakeSignalAgent closes the
// connection instead of answering, as an agent that the guest init stopped.
const closeSession byte = 0xff

// fakeSignalAgent reads the open request from conn, checks it, and answers
// with typ and payload.
func fakeSignalAgent(t *testing.T, conn net.Conn, typ byte, payload any) {
	t.Helper()
	go func() {
		f, err := agentproto.ReadFrame(conn)
		if !assert.NoError(t, err) {
			return
		}
		assert.Equal(t, agentproto.TypeOpen, f.Type)
		var req agentproto.OpenRequest
		assert.NoError(t, json.Unmarshal(f.Payload, &req))
		assert.Equal(t, []string{"/run/urunc/urunit", "15"}, req.Argv)
		assert.Equal(t, []string{"URUNIT_MODE=signal"}, req.Env)
		assert.Empty(t, req.User, "the signal role runs as root")
		switch typ {
		case 0:
		case closeSession:
			_ = conn.Close()
		default:
			_ = agentproto.WriteJSON(conn, typ, f.Stream, payload)
		}
	}()
}

func TestSignalGuestInit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		typ     byte
		payload any
		wantErr string
	}{
		{"exit 0 delivers", agentproto.TypeExit, agentproto.Exit{Code: 0}, ""},
		{"a failed urunit is an error", agentproto.TypeExit, agentproto.Exit{Code: 2}, "exited with 2"},
		{"an agent error is an error", agentproto.TypeError, agentproto.Error{Message: "no such file"}, "no such file"},
		{"no answer times out", 0, nil, "no answer"},
		{"a closed session is unconfirmed", closeSession, nil, unikontainers.ErrGuestSignalUnconfirmed.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host, guest := net.Pipe()
			defer guest.Close()
			fakeSignalAgent(t, guest, tc.typ, tc.payload)
			err := signalGuestInit(host, unix.SIGTERM, 300*time.Millisecond)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
