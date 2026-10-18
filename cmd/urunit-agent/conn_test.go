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

//go:build linux

package main

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/urunc-dev/urunc/pkg/agentproto"
)

func TestClosedConnDropsFrames(t *testing.T) {
	var out bytes.Buffer
	c := &connState{w: &out, sessions: make(map[uint32]*session)}
	if err := c.writeFrame(agentproto.TypeExit, 1, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	n := out.Len()
	c.closed.Store(true)
	if err := c.writeFrame(agentproto.TypeExit, 1, []byte("{}")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("writeFrame after close = %v, want ErrClosedPipe", err)
	}
	if out.Len() != n {
		t.Fatalf("%d bytes written after close", out.Len()-n)
	}
}
