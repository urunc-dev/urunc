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
	"time"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const (
	// InNsAPISockPath is the path of the VMM control API socket, as seen
	// from inside the monitor's mount namespace. The monitor rootfs always
	// contains a /tmp directory, so the socket is placed there. From the
	// host, the socket is reachable via /proc/<pid>/root + InNsAPISockPath.
	InNsAPISockPath = "/tmp/urunc-vmm-api.sock"

	// InNsSnapshotDir is the directory, inside the monitor's mount
	// namespace, where snapshot files are written on checkpoint and staged
	// on restore.
	InNsSnapshotDir = "/urunc-snapshot"
)

// ErrSnapshotNotSupported is returned when checkpoint/restore is requested
// for a VMM that has no snapshot capability (e.g. solo5-spt, solo5-hvt,
// hedge) or one for which urunc has not implemented it yet (e.g. QEMU).
var ErrSnapshotNotSupported = errors.New("the VMM does not support snapshot/restore")

// NetOverride describes how the network device of a snapshotted VM must be
// re-wired at restore time. The guest-visible device (MAC, IP, queues) is
// frozen in the snapshot; only the host-side backend (the tap device name)
// can change between checkpoint and restore.
type NetOverride struct {
	// IfaceID is the VMM-internal network interface identifier
	// (e.g. "net1" for Firecracker).
	IfaceID string
	// TapDev is the name of the freshly-created host tap device the
	// restored VM must attach to.
	TapDev string
}

// WaitForVMMAPI waits until the VMM API socket at sockPath (a host-resolvable
// path) accepts connections, or fails after timeout. Callers use it after
// spawning a fresh VMM process, before driving its API.
func WaitForVMMAPI(sockPath string, timeout time.Duration) error {
	return newVMMAPIClient(sockPath).waitForSocket(timeout)
}

// Snapshotter is the optional capability interface a VMM backend implements
// to support checkpoint/restore. Checkpoint maps to "pause the VM and write
// its full state (device model + guest memory) to a directory"; restore maps
// to "launch a fresh VMM process and resume the VM from that directory".
//
// Path arguments are always expressed as seen from inside the monitor's
// mount namespace (the VMM resolves them), while sockPath arguments are
// host-resolvable paths to the API socket (typically via /proc/<pid>/root).
type Snapshotter interface {
	// SupportsSnapshot reports whether this backend can checkpoint/restore.
	SupportsSnapshot() bool
	// PauseVM pauses the vCPUs of a running VM.
	PauseVM(sockPath string) error
	// ResumeVM resumes the vCPUs of a paused VM.
	ResumeVM(sockPath string) error
	// SnapshotVM writes a full snapshot of a paused VM into inNsDir.
	SnapshotVM(sockPath string, inNsDir string) error
	// BuildRestoreCmd builds the argv to launch a fresh VMM process that
	// will restore the VM from the snapshot staged in inNsDir. Depending
	// on the backend, the actual state load happens either at process
	// start (Cloud Hypervisor --restore) or via a later FinishRestore
	// API call (Firecracker PUT /snapshot/load).
	BuildRestoreCmd(args types.ExecArgs, inNsDir string) ([]string, error)
	// PrepareRestore performs backend-specific fixups on the staged
	// snapshot directory (hostDir is the host-resolvable path of the
	// staged copy) before the VMM process is launched. For example,
	// Cloud Hypervisor needs the tap device name in the snapshot's
	// config.json rewritten to the freshly-created tap.
	PrepareRestore(hostDir string, net NetOverride) error
	// FinishRestore completes the restore against the VMM API after the
	// fresh VMM process is up: for Firecracker it loads the snapshot and
	// resumes; for Cloud Hypervisor it resumes the already-restored VM.
	FinishRestore(sockPath string, inNsDir string, net NetOverride) error
}
