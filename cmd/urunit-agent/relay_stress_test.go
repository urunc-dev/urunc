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
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// stressSession runs one short command on a fresh pty, the way open()
// does, and returns the bytes relayOutput delivered.
func stressSession(id int) (string, error) {
	ptmx, tts, err := pty.Open()
	if err != nil {
		return "", fmt.Errorf("open pty: %w", err)
	}
	defer ptmx.Close()
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	script := fmt.Sprintf("echo A-%d; echo B-%d", id, id)
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tts, tts, tts
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = tts.Close()
		return "", fmt.Errorf("start: %w", err)
	}
	_ = tts.Close()

	var mu sync.Mutex
	var out strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		relayOutput(ptmx, true, func(b []byte) error {
			mu.Lock()
			out.Write(b)
			mu.Unlock()
			return nil
		})
	}()
	_ = cmd.Wait()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		return "", fmt.Errorf("relay did not end within 5s")
	}
	mu.Lock()
	defer mu.Unlock()
	return out.String(), nil
}

func TestRelayOutputPtyStress(t *testing.T) {
	if testing.Short() {
		t.Skip("pty stress test skipped under -short")
	}
	const workers, per = 8, 250
	var wg sync.WaitGroup
	errs := make(chan string, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				id := w*per + i
				got, err := stressSession(id)
				if err != nil {
					errs <- fmt.Sprintf("session %d: %v", id, err)
					return
				}
				if !strings.Contains(got, fmt.Sprintf("A-%d", id)) || !strings.Contains(got, fmt.Sprintf("B-%d", id)) {
					errs <- fmt.Sprintf("session %d lost output, got %q", id, got)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	if e, ok := <-errs; ok {
		t.Fatal(e)
	}
}
