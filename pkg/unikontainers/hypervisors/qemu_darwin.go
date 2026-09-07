//go:build darwin
// +build darwin

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
	"path/filepath"
	"runtime"
)

// NewQemuDarwin returns the shared Qemu backend configured for macOS:
// Hypervisor.framework acceleration, Homebrew firmware, and a QMP control
// socket. It is the same *Qemu type the Linux factory builds — only the
// platform fields differ.
func NewQemuDarwin(binaryPath string) *Qemu {
	return &Qemu{
		binary:     "qemu-system-aarch64",
		binaryPath: binaryPath,
		vhost:      false,
		darwin:     true,
		accel:      "-accel hvf",
		firmware:   "/opt/homebrew/share/qemu",
		qmpSocket:  filepath.Join(os.TempDir(), "urunc-qemu.sock"),
	}
}

// IsHVFAvailable reports whether Hypervisor.framework acceleration is usable
// (Apple Silicon).
func IsHVFAvailable() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}
