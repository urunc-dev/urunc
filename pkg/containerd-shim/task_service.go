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

package containerdshim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/log"
	"github.com/containerd/ttrpc"
	containerdShim "github.com/urunc-dev/urunc/pkg/containerd-shim/containerd"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
)

// Internal bundle annotation (duplicated in unikontainers; keep in sync).
const annotRootfsParams = "com.urunc.internal.rootfs.params"

// taskService is urunc's shim-side wrapper around containerd's runc task
// service. It wires urunc task setup before forwarding calls to the wrapped
// service.
type taskService struct {
	taskAPI.TaskService

	containerdAddress string
	// Used on Delete, where cwd may no longer be the bundle.
	stateRoot string
}

func (s *taskService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	session, err := containerdShim.OpenSession(ctx, s.containerdAddress, r.ID)
	if err != nil {
		log.G(ctx).WithError(err).Warn("urunc(shim): failed to open containerd session")
	} else {
		defer func() {
			if err := session.Close(); err != nil {
				log.G(ctx).WithError(err).Warn("urunc(shim): failed to close containerd session")
			}
		}()
		if err := containerdShim.InjectUruncAnnotations(ctx, session, r.Bundle); err != nil {
			log.G(ctx).WithError(err).Warn("urunc(shim): failed to inject annotations to spec")
		}
	}

	resp, err := s.TaskService.Create(ctx, r)
	if err != nil {
		return resp, err
	}

	rootfsChoice, err := chooseGuestRootfs(r)
	if err != nil {
		if errors.Is(err, errGuestRootfsChoiceSkipped) {
			log.G(ctx).WithError(err).Debug("urunc(shim): guest rootfs choice skipped")
			return resp, nil
		}
		log.G(ctx).WithError(err).Warn("urunc(shim): failed to choose guest rootfs")
		return nil, err
	}

	rootfsViewState, shouldPrepareRootfsView, err := containerdShim.PrepareRootfsView(ctx, session, rootfsChoice, r.Bundle)
	rootfsViewPrepared := false
	if err != nil {
		if shouldPrepareRootfsView {
			// Preflight passed and prepare failed. This is non-fatal: the runtime can
			// still fall back to extracting boot artifacts from the legacy mounted rootfs.
			log.G(ctx).WithError(err).Warn("urunc(shim): failed to prepare rootfs view; falling back to legacy boot artifact extraction")
		} else {
			// A disabled rootfs_view returns nil error and is handled as a skipped
			// prepare below; this branch means the enablement check itself failed.
			log.G(ctx).WithError(err).Warn("urunc(shim): failed to check rootfs view config; rootfs view skipped")
		}
	} else if shouldPrepareRootfsView {
		rootfsViewPrepared = true
		log.G(ctx).Debug("urunc(shim): rootfs view prepared")
	} else if session != nil {
		log.G(ctx).WithField("rootfs_type", rootfsChoice.Type).Debug("urunc(shim): rootfs view prepare skipped")
	}

	rootfsViewPersisted := false
	defer func() {
		if rootfsViewPrepared && !rootfsViewPersisted {
			cleanupRootfsView(ctx, session, "", rootfsViewState.Mountpoint, "create rollback")
		}
	}()

	rootfsParamsJSON, err := json.Marshal(rootfsChoice)
	if err != nil {
		log.G(ctx).WithError(err).Warn("urunc(shim): failed to encode rootfs params")
		return nil, err
	}

	if err := containerdShim.PatchConfigJSON(r.Bundle, map[string]string{
		annotRootfsParams: string(rootfsParamsJSON),
	}); err != nil {
		log.G(ctx).WithError(err).Warn("urunc(shim): failed to persist shim create annotations")
		return nil, err
	}

	if rootfsViewPrepared {
		if err := unikontainers.WriteRootfsViewState(r.Bundle, rootfsViewState); err != nil {
			log.G(ctx).WithError(err).Warn("urunc(shim): failed to persist rootfs view state")
			return nil, err
		}
		rootfsViewPersisted = true
	}

	return resp, nil
}

func cleanupRootfsView(ctx context.Context, session *containerdShim.Session, snapshotter, mountpoint, reason string) {
	if err := containerdShim.CleanupRootfsView(ctx, session, snapshotter, mountpoint); err != nil {
		log.G(ctx).WithError(err).WithField("reason", reason).Warn("urunc(shim): failed to clean up rootfs view")
	}
}

func (s *taskService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	shouldCleanup := false
	snapshotter := ""
	rootfsViewMountpoint := ""
	var loadErr error

	if r.ExecID == "" {
		bundle, err := s.bundlePathFor(ctx, r.ID)
		if err != nil {
			log.G(ctx).WithError(err).Warn("urunc(shim): resolve bundle path during Delete failed")
			loadErr = err
		} else {
			// Read view state before inner Delete; snapshotter is taken from bundle
			// (written at Prepare) because container metadata may be gone after Delete.
			var mountpoint string
			shouldCleanup, snapshotter, mountpoint, loadErr = containerdShim.ShouldCleanupRootfsView(bundle)
			if loadErr == nil {
				rootfsViewMountpoint = mountpoint
			}
		}
	}

	// Delete tears down the monitor namespace before removing the view it may pin.
	resp, err := s.TaskService.Delete(ctx, r)

	if loadErr != nil {
		if err != nil {
			return resp, err
		}
		return resp, loadErr
	}

	if shouldCleanup {
		session, sessionErr := containerdShim.OpenSession(ctx, s.containerdAddress, r.ID)
		if sessionErr != nil {
			log.G(ctx).WithError(sessionErr).Warn("urunc(shim): open containerd session for rootfs view cleanup failed")
			if err == nil {
				err = sessionErr
			}
		} else {
			defer func() {
				if err := session.Close(); err != nil {
					log.G(ctx).WithError(err).Warn("urunc(shim): failed to close containerd session after rootfs view cleanup")
				}
			}()
			if cleanupErr := containerdShim.CleanupRootfsView(ctx, session, snapshotter, rootfsViewMountpoint); cleanupErr != nil {
				log.G(ctx).WithError(cleanupErr).Warn("urunc(shim): delete rootfs view during Delete failed")
				if err == nil {
					err = cleanupErr
				}
			}
		}
	}

	return resp, err
}

func (s *taskService) RegisterTTRPC(server *ttrpc.Server) error {
	taskAPI.RegisterTaskService(server, s)
	return nil
}

func (s *taskService) bundlePathFor(ctx context.Context, containerID string) (string, error) {
	if s.stateRoot == "" {
		return "", fmt.Errorf("task service state root is empty (shim cwd layout assumption violated)")
	}
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return "", fmt.Errorf("namespace required: %w", err)
	}
	return filepath.Join(s.stateRoot, ns, containerID), nil
}
