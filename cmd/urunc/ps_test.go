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
	"os"
	"os/exec"
	"testing"
)

// TestGetAllDescendants_IncludesRoot verifies that the root PID
// itself is always included in the result.
func TestGetAllDescendants_IncludesRoot(t *testing.T) {
	pid := os.Getpid()
	pids := getAllDescendants(pid)

	if len(pids) == 0 {
		t.Fatal("expected at least one PID, got none")
	}
	if pids[0] != pid {
		t.Errorf("expected first PID to be root %d, got %d", pid, pids[0])
	}
}

// TestGetAllDescendants_NonExistentPID verifies that a non-existent
// PID returns only itself without crashing.
func TestGetAllDescendants_NonExistentPID(t *testing.T) {
	pids := getAllDescendants(99999999)

	if len(pids) != 1 {
		t.Errorf("expected 1 PID for non-existent process, got %d", len(pids))
	}
	if pids[0] != 99999999 {
		t.Errorf("expected PID 99999999, got %d", pids[0])
	}
}

// TestGetAllDescendants_IncludesChild verifies that a spawned child
// process appears in the descendants list.
func TestGetAllDescendants_IncludesChild(t *testing.T) {
	// Spawn a child process that sleeps long enough for us to inspect
	child := exec.Command("sleep", "10")
	if err := child.Start(); err != nil {
		t.Fatalf("failed to start child process: %v", err)
	}
	defer child.Process.Kill() //nolint:errcheck

	childPid := child.Process.Pid
	parentPid := os.Getpid()

	pids := getAllDescendants(parentPid)

	found := false
	for _, p := range pids {
		if p == childPid {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("child PID %d not found in descendants of %d: %v", childPid, parentPid, pids)
	}
}
