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
	"time"

	containersapi "github.com/containerd/containerd/api/services/containers/v1"
	contentapi "github.com/containerd/containerd/api/services/content/v1"
	imagesapi "github.com/containerd/containerd/api/services/images/v1"
	leasesapi "github.com/containerd/containerd/api/services/leases/v1"
	snapshotsapi "github.com/containerd/containerd/api/services/snapshots/v1"
	"github.com/containerd/containerd/defaults"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/dialer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultConnectTimeout = 10 * time.Second

type Session struct {
	conn *grpc.ClientConn

	namespace   string
	containerID string
	container   *containersapi.Container
}

// OpenSession opens a containerd session and loads the task container metadata.
func OpenSession(ctx context.Context, address, containerID string) (*Session, error) {
	if address == "" {
		return nil, fmt.Errorf("containerd address is empty")
	}
	if containerID == "" {
		return nil, fmt.Errorf("container id is empty")
	}

	namespace, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}

	backoffConfig := backoff.DefaultConfig
	backoffConfig.MaxDelay = 3 * time.Second
	dialOptions := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.FailOnNonTempDialError(true),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoffConfig}),
		grpc.WithContextDialer(dialer.ContextDialer),
		grpc.WithReturnConnectionError(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(defaults.DefaultMaxRecvMsgSize),
			grpc.MaxCallSendMsgSize(defaults.DefaultMaxSendMsgSize),
		),
	}

	dialCtx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	conn, err := grpc.DialContext(dialCtx, dialer.DialAddress(address), dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("dial containerd at %q: %w", address, err)
	}

	session := &Session{
		conn:        conn,
		namespace:   namespace,
		containerID: containerID,
	}

	container, err := loadContainer(ctx, namespace, containerID, session.containersClient())
	if err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			return nil, fmt.Errorf("loadContainer failed: %w; close containerd connection: %v", err, closeErr)
		}
		return nil, fmt.Errorf("loadContainer failed: %w", err)
	}
	session.container = container

	return session, nil
}

func (s *Session) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s *Session) GetNamespace() string {
	return s.namespace
}

func (s *Session) GetContainerID() string {
	return s.containerID
}

func (s *Session) GetContainer() *containersapi.Container {
	return s.container
}

func loadContainer(ctx context.Context, namespace, containerID string, client containersapi.ContainersClient) (*containersapi.Container, error) {
	resp, err := client.Get(withNamespace(ctx, namespace), &containersapi.GetContainerRequest{
		ID: containerID,
	})
	if err != nil {
		return nil, fmt.Errorf("get container %q: %w", containerID, containerdErr(err))
	}
	container := resp.GetContainer()
	if container == nil {
		return nil, fmt.Errorf("get container %q: response missing container", containerID)
	}

	return container, nil
}

func withNamespace(ctx context.Context, namespace string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return namespaces.WithNamespace(ctx, namespace)
}

func containerdErr(err error) error {
	if err == nil {
		return nil
	}
	return errdefs.FromGRPC(err)
}

func (s *Session) containersClient() containersapi.ContainersClient {
	return containersapi.NewContainersClient(s.conn)
}

func (s *Session) imagesClient() imagesapi.ImagesClient {
	return imagesapi.NewImagesClient(s.conn)
}

func (s *Session) contentClient() contentapi.ContentClient {
	return contentapi.NewContentClient(s.conn)
}

//nolint:unused // Used by follow-up feature-specific access constructors.
func (s *Session) snapshotsClient() snapshotsapi.SnapshotsClient {
	return snapshotsapi.NewSnapshotsClient(s.conn)
}

//nolint:unused // Used by follow-up feature-specific access constructors.
func (s *Session) leasesClient() leasesapi.LeasesClient {
	return leasesapi.NewLeasesClient(s.conn)
}
