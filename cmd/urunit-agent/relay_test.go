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
	"errors"
	"io"
	"reflect"
	"syscall"
	"testing"
)

// readStep is what one Read of a scriptedReader returns.
type readStep struct {
	data string
	err  error
}

// scriptedReader returns its steps in order, one per Read, and io.EOF once
// they run out. calls counts the reads.
type scriptedReader struct {
	steps []readStep
	calls int
}

func (s *scriptedReader) Read(p []byte) (int, error) {
	if s.calls >= len(s.steps) {
		s.calls++
		return 0, io.EOF
	}
	st := s.steps[s.calls]
	s.calls++
	n := copy(p, st.data)
	return n, st.err
}

// collect returns a write func for relayOutput that keeps a copy of every
// chunk it is given.
func collect() (func([]byte) error, *[]string) {
	chunks := &[]string{}
	return func(b []byte) error {
		*chunks = append(*chunks, string(b))
		return nil
	}, chunks
}

func TestRelayOutputTTYReadsOnceMoreAfterEIO(t *testing.T) {
	r := &scriptedReader{steps: []readStep{
		{data: "w1-1\r\n"},
		{err: syscall.EIO},
		{data: "done-w1-1\r\n"},
		{err: syscall.EIO},
	}}
	write, chunks := collect()
	relayOutput(r, true, write)
	want := []string{"w1-1\r\n", "done-w1-1\r\n"}
	if !reflect.DeepEqual(*chunks, want) {
		t.Fatalf("tty relay wrote %q, want %q", *chunks, want)
	}
	if r.calls != 4 {
		t.Fatalf("tty relay read %d times, want 4", r.calls)
	}
}

func TestRelayOutputTTYEndsOnSecondEIO(t *testing.T) {
	r := &scriptedReader{steps: []readStep{
		{err: syscall.EIO},
		{err: syscall.EIO},
		{data: "late\n"},
	}}
	write, chunks := collect()
	relayOutput(r, true, write)
	if len(*chunks) != 0 {
		t.Fatalf("tty relay wrote %q, want nothing", *chunks)
	}
	if r.calls != 2 {
		t.Fatalf("tty relay read %d times, want 2", r.calls)
	}
}

func TestRelayOutputTTYEndsOnEOF(t *testing.T) {
	r := &scriptedReader{steps: []readStep{
		{err: io.EOF},
		{data: "late\n"},
	}}
	write, chunks := collect()
	relayOutput(r, true, write)
	if len(*chunks) != 0 {
		t.Fatalf("tty relay wrote %q after EOF, want nothing", *chunks)
	}
	if r.calls != 1 {
		t.Fatalf("tty relay read %d times, want 1", r.calls)
	}
}

func TestRelayOutputPipeEndsOnEIO(t *testing.T) {
	r := &scriptedReader{steps: []readStep{
		{err: syscall.EIO},
		{data: "late\n"},
	}}
	write, chunks := collect()
	relayOutput(r, false, write)
	if len(*chunks) != 0 {
		t.Fatalf("pipe relay wrote %q after EIO, want nothing", *chunks)
	}
	if r.calls != 1 {
		t.Fatalf("pipe relay read %d times, want 1", r.calls)
	}
}

func TestRelayOutputEndsOnWriteError(t *testing.T) {
	r := &scriptedReader{steps: []readStep{
		{data: "a\n"},
		{data: "b\n"},
	}}
	writes := 0
	relayOutput(r, true, func([]byte) error {
		writes++
		return errors.New("peer gone")
	})
	if writes != 1 {
		t.Fatalf("relay called write %d times, want 1", writes)
	}
	if r.calls != 1 {
		t.Fatalf("relay read %d times after a write error, want 1", r.calls)
	}
}
