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
	"runtime"
	"strconv"
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

func BytesToBString(argMem uint64) string {
	if argMem == 0 {
		return strconv.FormatUint(DefaultMemory*1024*1024, 10)
	}
	return strconv.FormatUint(argMem, 10)
}

func BytesToMiBString(argMem uint64) string {
	if argMem == 0 {
		return strconv.FormatUint(DefaultMemory, 10)
	}
	userMem := bytesToMiB(argMem)
	return strconv.FormatUint(userMem, 10)
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
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for pid %d to die", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}
