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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/unix"
)

func TestBytesToMiB(t *testing.T) {
	t.Parallel()

	const mib uint64 = 1024 * 1024

	cases := []struct {
		name     string
		input    uint64
		expected uint64
	}{
		{"zero", 0, 0},
		{"less than one MiB truncates to zero", mib - 1, 0},
		{"exactly one MiB", mib, 1},
		{"exactly two MiB", 2 * mib, 2},
		{"non-multiple truncates down", mib + (mib / 2), 1},
		{"large value", 1024 * mib, 1024},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, bytesToMiB(tc.input))
		})
	}
}

func TestBytesToMB(t *testing.T) {
	t.Parallel()

	const mb uint64 = 1000 * 1000

	cases := []struct {
		name     string
		input    uint64
		expected uint64
	}{
		{"zero", 0, 0},
		{"less than one MB truncates to zero", mb - 1, 0},
		{"exactly one MB", mb, 1},
		{"exactly two MB", 2 * mb, 2},
		{"non-multiple truncates down", mb + (mb / 2), 1},
		{"large value", 1024 * mb, 1024},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, bytesToMB(tc.input))
		})
	}
}

// stubSysKill replaces sysKill with a recorder for the duration of the test.
// Tests using it must not call t.Parallel.
func stubSysKill(t *testing.T) *[][2]int {
	t.Helper()
	calls := &[][2]int{}
	orig := sysKill
	sysKill = func(pid int, sig unix.Signal) error {
		*calls = append(*calls, [2]int{pid, int(sig)})
		return nil
	}
	t.Cleanup(func() { sysKill = orig })
	return calls
}

func TestSignalProcessRefusesInvalidPid(t *testing.T) {
	for _, pid := range []int{-1, 0} {
		calls := stubSysKill(t)
		err := signalProcess(pid, unix.SIGKILL)
		assert.Error(t, err, "pid %d", pid)
		assert.Empty(t, *calls, "pid %d", pid)
	}
}

func TestKillProcessRefusesInvalidPid(t *testing.T) {
	for _, pid := range []int{-1, 0} {
		calls := stubSysKill(t)
		err := killProcess(pid)
		assert.Error(t, err, "pid %d", pid)
		assert.Empty(t, *calls, "pid %d", pid)
	}
}

func TestVMMSignalStopRefuseInvalidPid(t *testing.T) {
	vmms := map[string]interface {
		Signal(int, unix.Signal) error
		Stop(int) error
	}{
		"cloud-hypervisor": &CloudHypervisor{},
		"qemu":             &Qemu{},
		"firecracker":      &Firecracker{},
		"hvt":              &HVT{},
		"spt":              &SPT{},
		"hyperlight":       &Hyperlight{},
	}
	for name, vmm := range vmms {
		for _, pid := range []int{-1, 0} {
			calls := stubSysKill(t)
			assert.Error(t, vmm.Signal(pid, unix.SIGKILL), "%s Signal(%d, SIGKILL)", name, pid)
			assert.Error(t, vmm.Signal(pid, unix.SIGTERM), "%s Signal(%d, SIGTERM)", name, pid)
			assert.Error(t, vmm.Stop(pid), "%s Stop(%d)", name, pid)
			assert.Empty(t, *calls, "%s pid %d", name, pid)
		}
	}
}

func TestSignalProcessReachesSysKill(t *testing.T) {
	calls := stubSysKill(t)
	pid := os.Getpid()
	assert.NoError(t, signalProcess(pid, 0))
	assert.Equal(t, [][2]int{{pid, 0}}, *calls)
}
