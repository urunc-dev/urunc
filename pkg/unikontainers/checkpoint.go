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

package unikontainers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const (
	// annotRestorePath holds the host path of the checkpoint image
	// directory when a container is being restored instead of cold-booted.
	// It is set by `urunc restore` before the reexec process starts and is
	// consumed by Exec to branch into the restore path.
	annotRestorePath = "com.urunc.internal.restore.path"

	// checkpointMetadataFile is the name of the urunc-specific metadata
	// file written inside a checkpoint image directory, next to the VMM
	// snapshot files.
	checkpointMetadataFile = "urunc-checkpoint.json"

	// netInfoFilename is written in the container base directory by the
	// reexec process right before it executes the VMM. It records the
	// network parameters (most importantly the tap device name) of the
	// spawned VM, which checkpoint and restore need later.
	netInfoFilename = "netinfo.json"

	// vmmAPIWaitTimeout bounds how long we wait for a freshly spawned
	// VMM process to expose its API socket. Cloud Hypervisor loads the
	// whole snapshot before serving the API, so this must accommodate
	// reading guest memory from disk.
	vmmAPIWaitTimeout = 60 * time.Second
)

// CheckpointMetadata describes a urunc checkpoint. It is stored as
// checkpointMetadataFile inside the checkpoint image directory and validated
// on restore.
type CheckpointMetadata struct {
	// Version of the metadata format.
	Version int `json:"version"`
	// VmmType is the hypervisor that produced the snapshot. A snapshot
	// can only be restored by the same hypervisor type (and, for
	// Firecracker, the same hypervisor version).
	VmmType string `json:"vmmType"`
	// UnikernelType is the guest type of the snapshotted container.
	UnikernelType string `json:"unikernelType"`
	// Net records the network parameters the VM was started with. The
	// guest side (MAC, IP, gateway) is frozen inside the snapshot; the
	// restore path must present an equivalent L3 environment.
	Net types.NetDevParams `json:"net"`
	// SnapshotFiles lists the snapshot files in the checkpoint directory.
	SnapshotFiles []string `json:"snapshotFiles"`
}

// vmmSnapshotter returns the container's VMM together with its Snapshotter
// capability, or ErrSnapshotNotSupported if the VMM cannot snapshot.
func (u *Unikontainer) vmmSnapshotter() (types.VMM, hypervisors.Snapshotter, error) {
	vmmType := u.State.Annotations[annotHypervisor]
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(vmmType), u.UruncCfg.Monitors)
	if err != nil {
		return nil, nil, err
	}
	snapshotter, ok := vmm.(hypervisors.Snapshotter)
	if !ok || !snapshotter.SupportsSnapshot() {
		return nil, nil, fmt.Errorf("%w: %s", hypervisors.ErrSnapshotNotSupported, vmmType)
	}
	return vmm, snapshotter, nil
}

// apiSockHostPath returns a host-resolvable path to the VMM API socket of the
// running container. The socket lives inside the monitor's mount namespace,
// so it is reached through the /proc/<pid>/root magic link.
func (u *Unikontainer) apiSockHostPath() string {
	return filepath.Join(fmt.Sprintf("/proc/%d/root", u.State.Pid), hypervisors.InNsAPISockPath)
}

// procRootDir returns the host-resolvable path of the monitor's root
// directory, through the /proc/<pid>/root magic link.
func (u *Unikontainer) procRootDir() string {
	return fmt.Sprintf("/proc/%d/root", u.State.Pid)
}

// PauseVM pauses the vCPUs of the container's VM through the VMM API.
func (u *Unikontainer) PauseVM() error {
	if !u.isRunning() {
		return fmt.Errorf("container %s is not running", u.State.ID)
	}
	_, snapshotter, err := u.vmmSnapshotter()
	if err != nil {
		return err
	}
	if err := snapshotter.PauseVM(u.apiSockHostPath()); err != nil {
		return err
	}
	u.State.Status = "paused"
	return u.saveContainerState()
}

// ResumeVM resumes the vCPUs of the container's paused VM through the VMM API.
func (u *Unikontainer) ResumeVM() error {
	if !u.isRunning() {
		return fmt.Errorf("container %s is not running", u.State.ID)
	}
	_, snapshotter, err := u.vmmSnapshotter()
	if err != nil {
		return err
	}
	if err := snapshotter.ResumeVM(u.apiSockHostPath()); err != nil {
		return err
	}
	u.State.Status = specs.StateRunning
	return u.saveContainerState()
}

// Checkpoint pauses the container's VM, writes a full snapshot of it (device
// state and guest memory) into imagePath along with urunc metadata, and then
// either resumes the VM (leaveRunning) or stops it, matching runc's
// checkpoint semantics.
func (u *Unikontainer) Checkpoint(imagePath string, leaveRunning bool) error {
	if !u.isRunning() {
		return fmt.Errorf("cannot checkpoint container %s: not running", u.State.ID)
	}
	_, snapshotter, err := u.vmmSnapshotter()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(imagePath, 0o700); err != nil {
		return fmt.Errorf("failed to create checkpoint directory %s: %w", imagePath, err)
	}

	sockPath := u.apiSockHostPath()
	if err := snapshotter.PauseVM(sockPath); err != nil {
		return fmt.Errorf("failed to pause VM: %w", err)
	}

	// The VMM writes the snapshot inside its own mount namespace; create
	// the target directory and collect the files through /proc/<pid>/root.
	hostSnapDir := filepath.Join(u.procRootDir(), hypervisors.InNsSnapshotDir)
	if err := os.MkdirAll(hostSnapDir, 0o700); err != nil {
		resumeErr := snapshotter.ResumeVM(sockPath)
		return fmt.Errorf("failed to create in-guest snapshot directory %s: %w (resume error: %v)",
			hostSnapDir, err, resumeErr)
	}

	if err := snapshotter.SnapshotVM(sockPath, hypervisors.InNsSnapshotDir); err != nil {
		resumeErr := snapshotter.ResumeVM(sockPath)
		return fmt.Errorf("failed to snapshot VM: %w (resume error: %v)", err, resumeErr)
	}

	snapshotFiles, err := moveDirContents(hostSnapDir, imagePath)
	if err != nil {
		return fmt.Errorf("failed to collect snapshot files: %w", err)
	}

	netParams, err := u.loadNetInfo()
	if err != nil {
		uniklog.Warnf("could not load network info for checkpoint: %v", err)
	}

	metadata := CheckpointMetadata{
		Version:       1,
		VmmType:       u.State.Annotations[annotHypervisor],
		UnikernelType: u.State.Annotations[annotType],
		Net:           netParams,
		SnapshotFiles: snapshotFiles,
	}
	if err := writeCheckpointMetadata(imagePath, metadata); err != nil {
		return err
	}

	if leaveRunning {
		if err := snapshotter.ResumeVM(sockPath); err != nil {
			return fmt.Errorf("failed to resume VM after checkpoint: %w", err)
		}
		return nil
	}

	// Match runc semantics: after a successful checkpoint the container
	// stops, so the shim observes a task exit.
	return u.Kill()
}

// FinishRestore completes a restore after the fresh VMM process is up: it
// waits for the VMM API socket and asks the VMM to load (Firecracker) or
// resume (Cloud Hypervisor) the staged snapshot, re-wiring the frozen guest
// network device to the freshly-created tap device.
func (u *Unikontainer) FinishRestore() error {
	_, snapshotter, err := u.vmmSnapshotter()
	if err != nil {
		return err
	}

	netParams, err := u.loadNetInfo()
	if err != nil {
		uniklog.Warnf("could not load network info for restore: %v", err)
	}

	sockPath := u.apiSockHostPath()
	if err := hypervisors.WaitForVMMAPI(sockPath, vmmAPIWaitTimeout); err != nil {
		return err
	}

	netOverride := hypervisors.NetOverride{
		IfaceID: "net1",
		TapDev:  netParams.TapDev,
	}
	if err := snapshotter.FinishRestore(sockPath, hypervisors.InNsSnapshotDir, netOverride); err != nil {
		return fmt.Errorf("failed to finish VM restore: %w", err)
	}
	return nil
}

// RestorePath returns the checkpoint image directory this container should be
// restored from, or an empty string for a regular cold boot.
func (u *Unikontainer) RestorePath() string {
	return u.State.Annotations[annotRestorePath]
}

// SetRestorePath marks this container to be restored from the checkpoint
// image directory at imagePath. It must be called before InitialSetup so the
// reexec process observes the annotation in state.json.
func (u *Unikontainer) SetRestorePath(imagePath string) {
	u.State.Annotations[annotRestorePath] = imagePath
}

// stageRestore copies the checkpoint image directory into the monitor rootfs
// (so the VMM can reach the snapshot files after pivoting) and performs
// backend-specific fixups on the staged copy. It runs in the reexec process
// before changeRoot, while host paths are still resolvable. It returns the
// staged directory as seen from inside the monitor's mount namespace.
func (u *Unikontainer) stageRestore(snapshotter hypervisors.Snapshotter, monRootfs string, netArgs types.NetDevParams) (string, error) {
	restorePath := u.RestorePath()

	metadata, err := readCheckpointMetadata(restorePath)
	if err != nil {
		return "", err
	}
	vmmType := u.State.Annotations[annotHypervisor]
	if metadata.VmmType != vmmType {
		return "", fmt.Errorf("checkpoint was taken with VMM %q but container uses %q",
			metadata.VmmType, vmmType)
	}
	if metadata.Net.TapDev != "" && netArgs.IP != "" && metadata.Net.IP != netArgs.IP {
		// The guest network stack is frozen in the snapshot: restoring
		// under a different IP means the guest will keep using the old
		// one. Warn instead of failing, as some setups (e.g. flat L2
		// with proxy-ARP) may still be reachable.
		uniklog.Warnf("restoring snapshot with guest IP %s in a namespace with IP %s: "+
			"the guest will keep its original address", metadata.Net.IP, netArgs.IP)
	}

	stagedHostDir := filepath.Join(monRootfs, hypervisors.InNsSnapshotDir)
	if err := os.MkdirAll(stagedHostDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create staging directory %s: %w", stagedHostDir, err)
	}
	if _, err := copyDirContents(restorePath, stagedHostDir); err != nil {
		return "", fmt.Errorf("failed to stage snapshot files: %w", err)
	}

	netOverride := hypervisors.NetOverride{
		IfaceID: "net1",
		TapDev:  netArgs.TapDev,
	}
	if err := snapshotter.PrepareRestore(stagedHostDir, netOverride); err != nil {
		return "", fmt.Errorf("failed to prepare staged snapshot: %w", err)
	}

	return hypervisors.InNsSnapshotDir, nil
}

// saveNetInfo persists the network parameters of the VM in the container
// base directory. It is called by the reexec process right before executing
// the VMM, so that later checkpoint/restore invocations know the tap device
// the VM is attached to.
func (u *Unikontainer) saveNetInfo(netArgs types.NetDevParams) error {
	data, err := json.Marshal(netArgs)
	if err != nil {
		return fmt.Errorf("failed to marshal network info: %w", err)
	}
	return os.WriteFile(filepath.Join(u.BaseDir, netInfoFilename), data, 0o644) //nolint: gosec
}

// loadNetInfo loads the network parameters persisted by saveNetInfo.
func (u *Unikontainer) loadNetInfo() (types.NetDevParams, error) {
	var netParams types.NetDevParams
	data, err := os.ReadFile(filepath.Join(u.BaseDir, netInfoFilename))
	if err != nil {
		return netParams, err
	}
	err = json.Unmarshal(data, &netParams)
	return netParams, err
}

func writeCheckpointMetadata(imagePath string, metadata CheckpointMetadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint metadata: %w", err)
	}
	metadataPath := filepath.Join(imagePath, checkpointMetadataFile)
	if err := os.WriteFile(metadataPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write checkpoint metadata %s: %w", metadataPath, err)
	}
	return nil
}

func readCheckpointMetadata(imagePath string) (CheckpointMetadata, error) {
	var metadata CheckpointMetadata
	metadataPath := filepath.Join(imagePath, checkpointMetadataFile)
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return metadata, fmt.Errorf("failed to read checkpoint metadata %s: %w", metadataPath, err)
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return metadata, fmt.Errorf("failed to parse checkpoint metadata %s: %w", metadataPath, err)
	}
	return metadata, nil
}

// copyDirContents copies all regular files directly under srcDir into dstDir
// and returns their names. Snapshot directories are flat (both Firecracker
// and Cloud Hypervisor write plain files), so no recursion is needed.
func copyDirContents(srcDir string, dstDir string) ([]string, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if err := copyFile(filepath.Join(srcDir, entry.Name()), filepath.Join(dstDir, entry.Name())); err != nil {
			return nil, err
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// moveDirContents copies all regular files from srcDir to dstDir and removes
// the originals. A plain rename cannot be used because source and destination
// live on different filesystems (the source is behind /proc/<pid>/root).
func moveDirContents(srcDir string, dstDir string) ([]string, error) {
	names, err := copyDirContents(srcDir, dstDir)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if err := os.Remove(filepath.Join(srcDir, name)); err != nil {
			uniklog.Warnf("failed to remove staged snapshot file %s: %v", name, err)
		}
	}
	return names, nil
}
