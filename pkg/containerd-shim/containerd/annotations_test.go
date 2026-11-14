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
	"errors"
	"io"
	"maps"
	"testing"

	contentapi "github.com/containerd/containerd/api/services/content/v1"
	typesapi "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"google.golang.org/grpc"
)

const (
	testNamespace = "k8s.io"
	testDigest    = "sha256:x"
	ociManifest   = "application/vnd.oci.image.manifest.v1+json"
	labelKey      = uruncPrefix + "a"
)

// fakeStore is a content store that holds one blob and sends it in chunks.
type fakeStore struct {
	contentapi.ContentClient
	grpc.ClientStream
	chunks       [][]byte
	size         int64
	readFails    bool
	streamBreaks bool // fail before the last chunk
	reads        int
	wrongRead    bool // a Read asked for another digest, size or namespace
}

func (s *fakeStore) Read(ctx context.Context, req *contentapi.ReadContentRequest, _ ...grpc.CallOption) (contentapi.Content_ReadClient, error) {
	s.reads++
	if ns, _ := namespaces.Namespace(ctx); ns != testNamespace || req.Digest != testDigest || req.Size != s.size {
		s.wrongRead = true
	}
	if s.readFails {
		return nil, errors.New("read failed")
	}
	return s, nil
}

func (s *fakeStore) Recv() (*contentapi.ReadContentResponse, error) {
	if len(s.chunks) == 0 {
		return nil, io.EOF
	}
	if s.streamBreaks && len(s.chunks) == 1 {
		return nil, errors.New("stream broke")
	}
	data := s.chunks[0]
	s.chunks = s.chunks[1:]
	return &contentapi.ReadContentResponse{Data: data}, nil
}

// fetch runs fetchUruncAnnotations with the label labelKey=label, one that only
// looks like a urunc label, and a manifest in a fake store.
func fetch(mediaType string, manifest []byte, store *fakeStore) (map[string]string, error) {
	store.size = int64(len(manifest))
	fetcher := &annotationFetcher{
		containerLabels: map[string]string{labelKey: "label", "com.urunc.unikernel-c": "3"},
		contentClient:   store,
		namespace:       testNamespace,
	}
	if mediaType != "" {
		fetcher.target = &typesapi.Descriptor{MediaType: mediaType, Digest: testDigest, Size: store.size}
	}
	return fetcher.fetchUruncAnnotations(context.Background())
}

func TestFetchUruncAnnotations(t *testing.T) {
	manifest := `{"annotations":{"` + labelKey + `":"manifest","` + uruncPrefix + `b":"2","com.urunc.c":"3"}}`
	tests := []struct {
		name      string
		mediaType string
		store     fakeStore
		want      map[string]string
		wantErr   bool
		wantReads int
	}{
		{name: "manifest overrides the label, in two chunks", mediaType: ociManifest,
			store:     fakeStore{chunks: [][]byte{[]byte(manifest[:7]), []byte(manifest[7:])}},
			want:      map[string]string{labelKey: "manifest", uruncPrefix + "b": "2"},
			wantReads: 1},
		{name: "docker manifest", mediaType: "application/vnd.docker.distribution.manifest.v2+json",
			store:     fakeStore{chunks: [][]byte{[]byte(manifest)}},
			want:      map[string]string{labelKey: "manifest", uruncPrefix + "b": "2"},
			wantReads: 1},
		{name: "index is not read", mediaType: "application/vnd.oci.image.index.v1+json",
			store: fakeStore{chunks: [][]byte{[]byte(manifest)}},
			want:  map[string]string{labelKey: "label"}},
		{name: "no image is not read",
			want: map[string]string{labelKey: "label"}},
		{name: "read fails", mediaType: ociManifest,
			store:   fakeStore{chunks: [][]byte{[]byte("{}")}, readFails: true},
			wantErr: true, wantReads: 1},
		{name: "stream breaks", mediaType: ociManifest,
			store:   fakeStore{chunks: [][]byte{[]byte("{}"), []byte("")}, streamBreaks: true},
			wantErr: true, wantReads: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var blob []byte
			for _, c := range tt.store.chunks {
				blob = append(blob, c...)
			}
			got, err := fetch(tt.mediaType, blob, &tt.store)
			if (err != nil) != tt.wantErr || (err != nil && got != nil) {
				t.Fatalf("got %v, error %v, want an error: %v", got, err, tt.wantErr)
			}
			if tt.store.reads != tt.wantReads || tt.store.wrongRead {
				t.Fatalf("store read %d times, want %d, wrong request: %v", tt.store.reads, tt.wantReads, tt.store.wrongRead)
			}
			if !tt.wantErr && !maps.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
