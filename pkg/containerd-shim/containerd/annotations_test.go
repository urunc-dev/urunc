//go:build linux

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/internal/constants"
)

// The shim takes urunc annotations from the container's labels, which
// nerdctl and ctr fill from the image's config, but never a host one.
func TestFetchUruncAnnotationsDropsHostKeys(t *testing.T) {
	labels := map[string]string{
		"com.urunc.unikernel.hypervisor": "hvi",
		"com.urunc.unikernel.binary":     "/unikernel",
		"org.opencontainers.image.title": "app",
	}
	for _, k := range constants.HostAnnotations {
		labels[k] = "/from/the/image"
	}
	f := &annotationFetcher{containerLabels: labels}
	got, err := f.fetchUruncAnnotations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"com.urunc.unikernel.hypervisor": "hvi",
		"com.urunc.unikernel.binary":     "/unikernel",
	}, got)
}

func TestIsImageAnnotation(t *testing.T) {
	assert.True(t, isImageAnnotation("com.urunc.unikernel.hypervisor"))
	assert.False(t, isImageAnnotation("com.urunc.unikernel.netSocket"))
	assert.False(t, isImageAnnotation("com.urunc.unikernel.bootKernel"))
	assert.False(t, isImageAnnotation("io.kubernetes.cri.container-name"))
}
