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
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func cpuArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	case "amd64":
		return "x86_64"
	default:
		return ""
	}
}

func appendNonEmpty(body, prefix, value string) string {
	if value != "" {
		return body + prefix + value
	}
	return body
}

func bytesToMiB(bytes uint64) uint64 {
	const bytesInMiB = 1024 * 1024
	return bytes / bytesInMiB
}

func bytesToMB(bytes uint64) uint64 {
	const bytesInMB = 1000 * 1000
	return bytes / bytesInMB
}

func BytesToStringMB(argMem uint64) string {
	stringMem := strconv.FormatUint(DefaultMemory, 10)
	if argMem != 0 {
		userMem := bytesToMB(argMem)
		// Check for too low memory
		if userMem == 0 {
			userMem = DefaultMemory
		}
		stringMem = strconv.FormatUint(userMem, 10)
	}

	return stringMem
}

func killProcess(pid int) error {
	const timeout = 2 * time.Second
	err := unix.Kill(pid, unix.SIGKILL)
	if err != nil {
		if errors.Is(err, unix.ESRCH) {
			// Process already dead, nothing to do
			return nil
		}
		return err
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := unix.Kill(pid, 0); err != nil {
			if errors.Is(err, unix.ESRCH) {
				// process is dead
				break
			}
			return fmt.Errorf("error checking if process with pid %d is alive: %w", pid, err)
		}
		// unix.Kill(pid, 0) also succeeds for a zombie (defunct) process: the
		// VMM has terminated but has not yet been reaped by its parent (the
		// containerd shim). Such a process is effectively dead, so treat it as
		// gone instead of blocking here for the full timeout.
		if isZombie(pid) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for pid %d to die", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// isZombie reports whether the process with the given pid is in the zombie
// (defunct) state, i.e. it has terminated but has not yet been reaped by its
// parent. If the process' stat file cannot be read (e.g. it has already been
// reaped), the process is considered gone.
func isZombie(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	return statIsZombie(data)
}

// statIsZombie reports whether the contents of a /proc/<pid>/stat file describe
// a process in the zombie ("Z") state. The state is the third field, but the
// second field (comm) is wrapped in parentheses and may itself contain spaces
// or ')', so the state is read as the first non-space byte after the final ')'.
func statIsZombie(stat []byte) bool {
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return false
	}
	return s[i+2] == 'Z'
}
