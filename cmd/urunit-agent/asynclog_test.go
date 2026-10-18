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
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"testing"
	"time"
)

// blockingSink is an io.Writer whose writes block until release is closed.
type blockingSink struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
	mu      sync.Mutex
	lines   [][]byte
}

// newBlockingSink returns a blockingSink that blocks every write.
func newBlockingSink() *blockingSink {
	return &blockingSink{
		release: make(chan struct{}),
		entered: make(chan struct{}),
	}
}

// Write blocks until release is closed and then records a copy of p.
func (s *blockingSink) Write(p []byte) (int, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	s.mu.Lock()
	s.lines = append(s.lines, append([]byte(nil), p...))
	s.mu.Unlock()
	return len(p), nil
}

// Entered returns a channel that is closed on the first Write call.
func (s *blockingSink) Entered() <-chan struct{} {
	return s.entered
}

// Lines returns a copy of the lines written so far.
func (s *blockingSink) Lines() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.lines...)
}

// swapLogOutput points the standard logger at w until the test ends.
//
// The logger is process-global, so tests that call it must not run in
// parallel.
func swapLogOutput(t *testing.T, w io.Writer) {
	t.Helper()
	log.SetOutput(w)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
}

// waitFor polls cond every 10 ms until it returns true or timeout passes.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAsyncWriterDropsAndCounts(t *testing.T) {
	const depth = 4
	sink := newBlockingSink()
	released := false
	t.Cleanup(func() {
		if !released {
			close(sink.release)
		}
	})
	a := newAsyncWriter(sink, depth)

	write := func(i int) {
		t.Helper()
		p := []byte(fmt.Sprintf("l%d\n", i))
		n, err := a.Write(p)
		if n != len(p) || err != nil {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", p, n, err, len(p))
		}
	}

	write(0)
	select {
	case <-sink.Entered():
	case <-time.After(2 * time.Second):
		t.Fatal("drain goroutine did not write l0 to the sink")
	}

	// l0 is in flight on the blocked sink and the queue is empty. Of the
	// next depth+3 lines, depth fill the queue and 3 are dropped.
	const extra = 3
	start := time.Now()
	for i := 1; i <= depth+extra; i++ {
		write(i)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("writes against a blocked sink took %v", elapsed)
	}

	waitFor(t, 2*time.Second, "the drop count", func() bool {
		return a.Dropped() == extra
	})

	close(sink.release)
	released = true
	waitFor(t, 2*time.Second, "the queued lines", func() bool {
		return len(sink.Lines()) == 1+depth
	})

	lines := sink.Lines()
	if got := string(lines[0]); got != "l0\n" {
		t.Errorf("line 0 = %q; want %q", got, "l0\n")
	}
	report := fmt.Sprintf(droppedFormat, extra)
	if !bytes.HasPrefix(lines[1], []byte(report)) || !bytes.HasSuffix(lines[1], []byte("l1\n")) {
		t.Errorf("line 1 = %q; want %q followed by %q", lines[1], report, "l1\n")
	}
	for i := 2; i < len(lines); i++ {
		if want := fmt.Sprintf("l%d\n", i); string(lines[i]) != want {
			t.Errorf("line %d = %q; want %q", i, lines[i], want)
		}
	}
	if n := a.Dropped(); n != 0 {
		t.Errorf("Dropped() = %d after the report; want 0", n)
	}
}
