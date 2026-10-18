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
	"fmt"
	"io"
	"sync/atomic"
)

// logQueueDepth is the number of log lines queued before the agent starts
// dropping them.
const logQueueDepth = 64

// droppedFormat is the line that reports how many log lines were dropped.
// The writer emits it, so it carries the logger's prefix itself.
const droppedFormat = "urunit-agent: dropped %d log lines\n"

// asyncWriter is an io.Writer that hands each line to one goroutine over a
// bounded queue.
//
// The goroutine writes the lines to w in order. A line that finds the queue
// full is dropped and counted. The count goes out ahead of the next line
// that gets through.
type asyncWriter struct {
	w       io.Writer
	queue   chan []byte
	dropped atomic.Uint64
}

// newAsyncWriter returns an asyncWriter over w that queues up to depth lines.
//
// It starts the goroutine that writes the queued lines to w.
func newAsyncWriter(w io.Writer, depth int) *asyncWriter {
	a := &asyncWriter{w: w, queue: make(chan []byte, depth)}
	go a.drain()
	return a
}

// Write queues a copy of p and returns len(p) and a nil error.
//
// It never writes to w. When the queue is full it drops p and counts it.
func (a *asyncWriter) Write(p []byte) (int, error) {
	line := append([]byte(nil), p...)
	select {
	case a.queue <- line:
	default:
		a.dropped.Add(1)
	}
	return len(p), nil
}

// drain writes each queued line to w, preceded by the dropped-lines report
// when lines were dropped since the last one.
//
// The report and the line go out in one Write call, so other console output
// cannot land between them. Errors from w are ignored.
func (a *asyncWriter) drain() {
	for line := range a.queue {
		if n := a.dropped.Swap(0); n > 0 {
			line = append([]byte(fmt.Sprintf(droppedFormat, n)), line...)
		}
		_, _ = a.w.Write(line)
	}
}

// Dropped returns the number of lines dropped since the last report.
func (a *asyncWriter) Dropped() uint64 {
	return a.dropped.Load()
}
