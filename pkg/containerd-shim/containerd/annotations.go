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
	"encoding/json"
	"fmt"
	"io"
	"strings"

	contentapi "github.com/containerd/containerd/api/services/content/v1"
	imagesapi "github.com/containerd/containerd/api/services/images/v1"
	typesapi "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/images"

	imagespec "github.com/opencontainers/image-spec/specs-go/v1"
	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
)

const uruncPrefix = "com.urunc.unikernel."

type annotationFetcher struct {
	containerLabels map[string]string
	contentClient   contentapi.ContentClient
	namespace       string
	target          *typesapi.Descriptor
}

func newAnnotationFetcher(ctx context.Context, session *Session) (*annotationFetcher, error) {
	container := session.GetContainer()
	if container == nil {
		return nil, fmt.Errorf("container metadata is not loaded")
	}

	fetcher := &annotationFetcher{
		containerLabels: container.Labels,
		contentClient:   session.contentClient(),
		namespace:       session.GetNamespace(),
	}

	if container.Image == "" {
		// TODO: Add Docker fallback. When Docker does not use containerd's image/content
		// store, the containerd container metadata may not include an image reference.
		// In that case, resolve the image reference through the Docker Engine API and
		// fetch manifest annotations from a Docker-compatible path.
		return fetcher, nil
	}

	imageResp, err := session.imagesClient().Get(
		withNamespace(ctx, session.GetNamespace()),
		&imagesapi.GetImageRequest{Name: container.Image},
	)
	if err != nil {
		return fetcher, nil
	}

	fetcher.target = imageResp.GetImage().GetTarget()
	return fetcher, nil
}

func InjectUruncAnnotations(ctx context.Context, session *Session, spec *runtimespec.Spec) (bool, error) {
	fetcher, err := newAnnotationFetcher(ctx, session)
	if err != nil {
		return false, fmt.Errorf("create annotation fetcher: %w", err)
	}
	annotations, err := fetcher.fetchUruncAnnotations(ctx)
	if err != nil {
		return false, fmt.Errorf("fetch urunc annotations: %w", err)
	}
	if len(annotations) == 0 {
		return false, nil
	}

	if spec.Annotations == nil {
		spec.Annotations = make(map[string]string)
	}

	changed := false
	for k, v := range annotations {
		if _, exists := spec.Annotations[k]; exists {
			continue
		}
		spec.Annotations[k] = v
		changed = true
	}

	return changed, nil
}

func (f *annotationFetcher) fetchUruncAnnotations(ctx context.Context) (map[string]string, error) {
	filtered := make(map[string]string)

	// Collect urunc annotations from container labels.
	for k, v := range f.containerLabels {
		if strings.HasPrefix(k, uruncPrefix) {
			filtered[k] = v
		}
	}

	// Collect urunc annotations from manifest
	if f.target == nil || !images.IsManifestType(f.target.MediaType) {
		// If the image target is missing or does not point to a manifest,
		// keep the labels collected so far.
		return filtered, nil
	}

	manifestRaw, err := readBlob(ctx, f.namespace, f.contentClient, f.target.Digest, f.target.Size)
	if err != nil {
		return nil, fmt.Errorf("read manifest blob: %w", err)
	}

	var manifest imagespec.Manifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	// Manifest annotations override config labels on duplicate keys.
	for k, v := range manifest.Annotations {
		if strings.HasPrefix(k, uruncPrefix) {
			filtered[k] = v
		}
	}

	return filtered, nil
}

// readBlob reads a blob with the given digest from containerd's content store
// and returns it as a byte slice.
func readBlob(ctx context.Context, namespace string, contentClient contentapi.ContentClient, digest string, size int64) ([]byte, error) {
	stream, err := contentClient.Read(withNamespace(ctx, namespace), &contentapi.ReadContentRequest{
		Digest: digest,
		Size:   size,
	})
	if err != nil {
		return nil, containerdErr(err)
	}

	var raw []byte
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, containerdErr(err)
		}
		raw = append(raw, resp.Data...)
	}

	return raw, nil
}
