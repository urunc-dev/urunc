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

package hypervisors

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// writeFakeBinary creates an executable file called name in dir and returns
// its path.
func writeFakeBinary(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755)) //nolint:gosec // a stub executable
	return path
}

func TestNewVMMUnknownType(t *testing.T) {
	vmm, err := NewVMM("x", nil)
	assert.Nil(t, vmm)
	require.Error(t, err)
	assert.Equal(t, `vmm "x" is not supported`, err.Error())
}

// Hedge has no binary to look up. Its Ok() fails without /dev/hedge, and
// NewVMM reports that as a missing monitor.
func TestNewVMMHedgeNotInstalled(t *testing.T) {
	vmm, err := NewVMM(HedgeVmm, nil)
	assert.Nil(t, vmm)
	assert.True(t, errors.Is(err, ErrVMMNotInstalled), "got %v", err)
}

func TestNewVMMQemuBinaryPathAndVhost(t *testing.T) {
	path := writeFakeBinary(t, t.TempDir(), "my-qemu")
	monitors := map[string]types.MonitorConfig{
		string(QemuVmm): {BinaryPath: path, Vhost: true},
	}

	vmm, err := NewVMM(QemuVmm, monitors)
	require.NoError(t, err)
	q, ok := vmm.(*Qemu)
	require.True(t, ok, "got %T", vmm)
	assert.Equal(t, path, q.Path())
	assert.Equal(t, QemuBinary, q.binary)
	assert.True(t, q.vhost)
}

func TestNewVMMMissingBinary(t *testing.T) {
	t.Setenv("PATH", "")

	vmm, err := NewVMM(FirecrackerVmm, nil)
	assert.Nil(t, vmm)
	assert.True(t, errors.Is(err, ErrVMMNotInstalled), "got %v", err)
}

// The qemu lookup appends the host CPU architecture to the binary name.
func TestNewVMMQemuArchSuffix(t *testing.T) {
	if cpuArch() == "" {
		t.Skip("no qemu binary suffix for this architecture")
	}
	dir := t.TempDir()
	path := writeFakeBinary(t, dir, QemuBinary+cpuArch())
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	vmm, err := NewVMM(QemuVmm, map[string]types.MonitorConfig{})
	require.NoError(t, err)
	q, ok := vmm.(*Qemu)
	require.True(t, ok, "got %T", vmm)
	assert.Equal(t, path, q.Path())
	assert.False(t, q.vhost)
}

func TestVMMFactoryQemuVhostFalse(t *testing.T) {
	t.Parallel()
	factory, exists := vmmFactories[QemuVmm]
	assert.True(t, exists, "qemu factory should exist")

	vmm := factory.createFunc(QemuBinary, "/usr/bin/qemu-system-x86_64", false)
	qemu, ok := vmm.(*Qemu)
	assert.True(t, ok, "factory should return *Qemu")
	assert.False(t, qemu.vhost, "vhost should be false when passed as false")
}

func TestVMMFactoryQemuVhostTrue(t *testing.T) {
	t.Parallel()
	factory, exists := vmmFactories[QemuVmm]
	assert.True(t, exists, "qemu factory should exist")

	vmm := factory.createFunc(QemuBinary, "/usr/bin/qemu-system-x86_64", true)
	qemu, ok := vmm.(*Qemu)
	assert.True(t, ok, "factory should return *Qemu")
	assert.True(t, qemu.vhost, "vhost should be true when passed as true")
}

func TestVMMFactoryNonQemuIgnoresVhost(t *testing.T) {
	t.Parallel()

	// SPT factory should ignore vhost parameter
	factory, exists := vmmFactories[SptVmm]
	assert.True(t, exists, "spt factory should exist")

	vmm := factory.createFunc(SptBinary, "/usr/bin/solo5-spt", true)
	_, ok := vmm.(*SPT)
	assert.True(t, ok, "factory should return *SPT")

	// HVT factory should ignore vhost parameter
	factory, exists = vmmFactories[HvtVmm]
	assert.True(t, exists, "hvt factory should exist")

	vmm = factory.createFunc(HvtBinary, "/usr/bin/solo5-hvt", true)
	_, ok = vmm.(*HVT)
	assert.True(t, ok, "factory should return *HVT")

	// Firecracker factory should ignore vhost parameter
	factory, exists = vmmFactories[FirecrackerVmm]
	assert.True(t, exists, "firecracker factory should exist")

	vmm = factory.createFunc(FirecrackerBinary, "/usr/bin/firecracker", true)
	_, ok = vmm.(*Firecracker)
	assert.True(t, ok, "factory should return *Firecracker")
}
