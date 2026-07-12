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

package containerd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	leasesapi "github.com/containerd/containerd/api/services/leases/v1"
	snapshotsapi "github.com/containerd/containerd/api/services/snapshots/v1"
	cntrtypes "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/mount"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/metadata"
)

const (
	rootfsViewKeyPrefix      = "urunc-rootfs-view-"
	rootfsViewLeasePrefix    = "urunc-rootfs-view-lease-"
	rootfsViewMountpointName = "rootfs-view-mount"
)

type rootfsViewAccessor struct {
	namespace   string
	containerID string
	snapshotter string
	snapshotKey string
	snapshots   snapshotsapi.SnapshotsClient
	leases      leasesapi.LeasesClient
}

func newRootfsViewAccessor(session *Session) *rootfsViewAccessor {
	a := &rootfsViewAccessor{
		namespace:   session.GetNamespace(),
		containerID: session.GetContainerID(),
		snapshots:   session.snapshotsClient(),
		leases:      session.leasesClient(),
	}
	ctr := session.GetContainer()
	if ctr != nil && ctr.GetSnapshotKey() != "" {
		a.snapshotter = ctr.GetSnapshotter()
		a.snapshotKey = ctr.GetSnapshotKey()
	}
	return a
}

// PrepareRootfsView prepares a rootfs view when the container and
// rootfs choice support it. The returned shouldPrepare value lets callers
// distinguish config/check failures from prepare failures for logging.
func PrepareRootfsView(ctx context.Context, session *Session, rootfs types.RootfsParams, bundle string) (types.RootfsViewState, bool, error) {
	if session == nil {
		return types.RootfsViewState{}, false, nil
	}

	accessor := newRootfsViewAccessor(session)
	shouldPrepare, err := accessor.shouldPrepare(rootfs)
	if err != nil {
		return types.RootfsViewState{}, false, err
	}
	if !shouldPrepare {
		return types.RootfsViewState{}, false, nil
	}

	state, err := accessor.prepare(ctx, bundle)
	if err != nil {
		return types.RootfsViewState{}, true, err
	}

	return state, true, nil
}

// CleanupRootfsView removes a rootfs view using container metadata from the
// session and cleanup state read from the bundle.
func CleanupRootfsView(ctx context.Context, session *Session, snapshotter, mountpoint string) error {
	if session == nil {
		return fmt.Errorf("containerd session is nil")
	}

	accessor := newRootfsViewAccessor(session)
	return accessor.cleanupRootfsView(ctx, snapshotter, mountpoint)
}

func (a *rootfsViewAccessor) shouldPrepare(rootfs types.RootfsParams) (bool, error) {
	if a == nil ||
		a.snapshotter == "" ||
		a.snapshotKey == "" ||
		(a.snapshotter != "devmapper" && a.snapshotter != "blockfile") ||
		rootfs.Type != "block" ||
		rootfs.MountedPath == "" {
		return false, nil
	}

	uruncCfg, cfgErr := unikontainers.LoadUruncConfig(unikontainers.UruncConfigPath)
	if cfgErr != nil {
		return false, cfgErr
	}
	return uruncCfg.RootfsView.Enabled, nil
}

// prepare records a read-only view of the committed rootfs snapshot for runtime use.
// On success it returns view state for the caller to persist in bundle rootfs-view.json.
func (a *rootfsViewAccessor) prepare(ctx context.Context, bundle string) (types.RootfsViewState, error) {
	if a == nil {
		return types.RootfsViewState{}, fmt.Errorf("rootfs view accessor is nil")
	}

	snapshotKey, err := a.resolveCommittedSnapshotBase(ctx, a.snapshotter, a.snapshotKey)
	if err != nil {
		return types.RootfsViewState{}, err
	}

	viewKey := rootfsViewKeyPrefix + a.containerID
	leaseID := rootfsViewLeasePrefix + a.containerID

	nsCtx := withNamespace(ctx, a.namespace)
	if _, err := a.leases.Create(nsCtx, &leasesapi.CreateRequest{ID: leaseID}); err != nil {
		err = containerdErr(err)
		if err != nil && !errdefs.IsAlreadyExists(err) {
			return types.RootfsViewState{}, fmt.Errorf("create rootfs view lease %s: %w", leaseID, err)
		}
	}

	leaseCtx := metadata.AppendToOutgoingContext(nsCtx, "containerd-lease", leaseID)
	mounts, err := a.createRootfsView(leaseCtx, viewKey, snapshotKey)
	if err != nil {
		_ = deleteRootfsViewLease(ctx, a.namespace, leaseID, a.leases)
		return types.RootfsViewState{}, err
	}

	mountpoint := filepath.Join(filepath.Clean(bundle), rootfsViewMountpointName)
	keepView := false
	defer func() {
		if !keepView {
			_ = cleanupRootfsViewMountpoint(mountpoint)
			_ = removeRootfsViewSnapshotAndLease(ctx, a.namespace, a.containerID, a.snapshotter, a.snapshots, a.leases)
		}
	}()

	if err := prepareRootfsViewMountpoint(mountpoint, mounts); err != nil {
		return types.RootfsViewState{}, err
	}

	keepView = true
	return types.RootfsViewState{
		Snapshotter: a.snapshotter,
		Mountpoint:  mountpoint,
		Mounts:      mounts,
	}, nil
}

// Rootfs view cleanup (call chain):
//
//	Delete / Stop:  ShouldCleanupRootfsView(bundle) → CleanupRootfsView(ctx, session, snapshotter, mountpoint)
//	Create rollback: CleanupRootfsView(ctx, session, "", state.Mountpoint)
//
//	cleanupRootfsView → removeRootfsViewSnapshotAndLease (view snapshot + lease in containerd)
//	prepare failure after lease create → deleteRootfsViewLease (lease only)

// cleanupRootfsView unmounts the shim-mounted rootfs view, then removes its snapshot and lease.
func (a *rootfsViewAccessor) cleanupRootfsView(ctx context.Context, snapshotter, mountpoint string) error {
	if a == nil {
		return fmt.Errorf("rootfs view accessor is nil")
	}
	if a.containerID == "" {
		return fmt.Errorf("container id is empty")
	}

	effectiveSnapshotter := snapshotter
	if effectiveSnapshotter == "" {
		effectiveSnapshotter = a.snapshotter
	}
	if effectiveSnapshotter == "" {
		return fmt.Errorf("snapshotter name required for rootfs view cleanup")
	}

	if err := cleanupRootfsViewMountpoint(mountpoint); err != nil {
		return err
	}

	return removeRootfsViewSnapshotAndLease(
		ctx, a.namespace, a.containerID, effectiveSnapshotter, a.snapshots, a.leases,
	)
}

func (a *rootfsViewAccessor) statSnapshot(ctx context.Context, snapshotter, key string) (parent string, committed bool, err error) {
	resp, err := a.snapshots.Stat(withNamespace(ctx, a.namespace), &snapshotsapi.StatSnapshotRequest{
		Snapshotter: snapshotter,
		Key:         key,
	})
	if err = containerdErr(err); err != nil {
		return "", false, err
	}
	info := resp.GetInfo()
	if info == nil {
		return "", false, fmt.Errorf("stat snapshot %s (%s): empty info", key, snapshotter)
	}
	return info.GetParent(), info.GetKind() == snapshotsapi.Kind_COMMITTED, nil
}

func (a *rootfsViewAccessor) resolveCommittedSnapshotBase(ctx context.Context, snapshotter, snapshotKey string) (string, error) {
	parent, committed, err := a.statSnapshot(ctx, snapshotter, snapshotKey)
	if err != nil {
		return "", fmt.Errorf("stat snapshot %s (%s): %w", snapshotKey, snapshotter, err)
	}
	if committed {
		return snapshotKey, nil
	}
	if parent == "" {
		return snapshotKey, nil
	}

	current := parent
	for {
		parent, committed, err = a.statSnapshot(ctx, snapshotter, current)
		if err != nil {
			return "", fmt.Errorf("stat snapshot %s (%s parent walk): %w", current, snapshotter, err)
		}
		if committed {
			return current, nil
		}
		if parent == "" {
			return "", fmt.Errorf("%s snapshot %s has no committed parent in chain", snapshotter, snapshotKey)
		}
		current = parent
	}
}

func (a *rootfsViewAccessor) createRootfsView(ctx context.Context, viewKey, parentKey string) ([]mount.Mount, error) {
	nsCtx := withNamespace(ctx, a.namespace)
	viewResp, err := a.snapshots.View(nsCtx, &snapshotsapi.ViewSnapshotRequest{
		Snapshotter: a.snapshotter,
		Key:         viewKey,
		Parent:      parentKey,
	})
	if err = containerdErr(err); err == nil {
		return protoMountsToMounts(viewResp.GetMounts()), nil
	}
	if !errdefs.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create rootfs view %s from %s: %w", viewKey, parentKey, err)
	}

	// Reuse an existing view left by a retry or partial prepare.
	mountsResp, err := a.snapshots.Mounts(nsCtx, &snapshotsapi.MountsRequest{
		Snapshotter: a.snapshotter,
		Key:         viewKey,
	})
	if err = containerdErr(err); err != nil {
		return nil, fmt.Errorf("create rootfs view %s from %s: %w", viewKey, parentKey, err)
	}
	return protoMountsToMounts(mountsResp.GetMounts()), nil
}

func protoMountsToMounts(mm []*cntrtypes.Mount) []mount.Mount {
	out := make([]mount.Mount, len(mm))
	for i, m := range mm {
		out[i] = mount.Mount{
			Type:    m.Type,
			Source:  m.Source,
			Target:  m.Target,
			Options: m.Options,
		}
	}
	return out
}

// ShouldCleanupRootfsView reports whether bundle rootfs-view.json exists and returns cleanup state.
func ShouldCleanupRootfsView(bundle string) (bool, string, string, error) {
	state, err := unikontainers.LoadRootfsViewState(bundle)
	if err != nil {
		return false, "", "", err
	}
	if state == nil || state.Snapshotter == "" {
		return false, "", "", nil
	}
	return true, state.Snapshotter, state.Mountpoint, nil
}

func prepareRootfsViewMountpoint(mountpoint string, mounts []mount.Mount) error {
	if err := cleanupRootfsViewMountpoint(mountpoint); err != nil {
		return err
	}
	if err := os.MkdirAll(mountpoint, 0o755); err != nil {
		return fmt.Errorf("create rootfs view mountpoint %s: %w", mountpoint, err)
	}
	if err := mount.All(mounts, mountpoint); err != nil {
		_ = cleanupRootfsViewMountpoint(mountpoint)
		return fmt.Errorf("mount rootfs view at %s: %w", mountpoint, err)
	}
	return nil
}

func cleanupRootfsViewMountpoint(mountpoint string) error {
	if mountpoint == "" {
		return nil
	}
	mountpoint = filepath.Clean(mountpoint)
	if err := mount.Unmount(mountpoint, 0); err != nil && !os.IsNotExist(err) && err != unix.EINVAL {
		return fmt.Errorf("unmount rootfs view mountpoint %s: %w", mountpoint, err)
	}
	if err := os.RemoveAll(mountpoint); err != nil {
		return fmt.Errorf("remove rootfs view mountpoint %s: %w", mountpoint, err)
	}
	return nil
}

// removeRootfsViewSnapshotAndLease deletes the view snapshot and its lease in containerd.
func removeRootfsViewSnapshotAndLease(
	ctx context.Context,
	namespace, containerID, snapshotter string,
	snapshots snapshotsapi.SnapshotsClient,
	leases leasesapi.LeasesClient,
) error {
	if containerID == "" || snapshotter == "" {
		return nil
	}
	nsCtx := withNamespace(ctx, namespace)
	_, err := snapshots.Remove(nsCtx, &snapshotsapi.RemoveSnapshotRequest{
		Snapshotter: snapshotter,
		Key:         rootfsViewKeyPrefix + containerID,
	})
	if err = containerdErr(err); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return deleteRootfsViewLease(ctx, namespace, rootfsViewLeasePrefix+containerID, leases)
}

// deleteRootfsViewLease removes only the containerd lease (Prepare rollback after lease create).
func deleteRootfsViewLease(ctx context.Context, namespace, leaseID string, leases leasesapi.LeasesClient) error {
	if leaseID == "" {
		return nil
	}
	_, err := leases.Delete(withNamespace(ctx, namespace), &leasesapi.DeleteRequest{ID: leaseID})
	if err = containerdErr(err); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}
