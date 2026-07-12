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
	"os"

	"github.com/containerd/containerd/runtime/v2/runc/manager"
	"github.com/containerd/containerd/runtime/v2/shim"
	"github.com/containerd/log"
	containerdShim "github.com/urunc-dev/urunc/pkg/containerd-shim/containerd"
)

const containerdGRPCAddressEnv = "GRPC_ADDRESS"

func containerdGRPCAddress() string {
	return os.Getenv(containerdGRPCAddressEnv)
}

type shimManager struct {
	shim.Manager
}

func NewShimManager(runtime string) shim.Manager {
	return &shimManager{Manager: manager.NewShimManager(runtime)}
}

func (m *shimManager) Stop(ctx context.Context, id string) (shim.StopStatus, error) {
	bundle, err := os.Getwd()
	if err != nil {
		log.G(ctx).WithError(err).Warn("urunc(shim): getwd during delete failed")
		return m.Manager.Stop(ctx, id)
	}

	shouldCleanup, snapshotter, mountpoint, err := containerdShim.ShouldCleanupRootfsView(bundle)
	if err != nil {
		log.G(ctx).WithError(err).Warn("urunc(shim): read rootfs view cleanup state from bundle during delete failed")
		return m.Manager.Stop(ctx, id)
	}
	if !shouldCleanup {
		return m.Manager.Stop(ctx, id)
	}

	address := containerdGRPCAddress()
	if address == "" {
		log.G(ctx).Warn("urunc(shim): containerd gRPC address unset during delete; rootfs view cleanup skipped")
		return m.Manager.Stop(ctx, id)
	}

	session, err := containerdShim.OpenSession(ctx, address, id)
	if err != nil {
		log.G(ctx).WithError(err).Warn("urunc(shim): open containerd session for rootfs view cleanup failed")
		return m.Manager.Stop(ctx, id)
	}
	defer func() {
		if err := session.Close(); err != nil {
			log.G(ctx).WithError(err).Warn("urunc(shim): failed to close containerd session after rootfs view cleanup")
		}
	}()

	// snapshotter from bundle view state; shim cwd may outlive task Delete.
	if err := containerdShim.CleanupRootfsView(ctx, session, snapshotter, mountpoint); err != nil {
		log.G(ctx).WithError(err).Warn("urunc(shim): rootfs view cleanup during delete failed")
	}

	return m.Manager.Stop(ctx, id)
}
