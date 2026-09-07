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

// TestNewAnnotationsSanity exercises New, the Linux orchestration entry point,
// so it lives in a Linux-only file; the annotation parsing tests themselves are
// platform-neutral and stay in annotations_test.go.

package unikontainers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewAnnotationsSanity(t *testing.T) {
	// An invalid annotation value must not end up as ErrNotUnikernel.
	// Otherwise urunc would hand the container over to runc and the actual
	// reason of the failure would get lost.
	t.Run("invalid annotation does not fall back to runc", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		annots[annotMountRootfs] = "yes"

		_, err := New(writeBundle(t, annots), "test-container", t.TempDir(), defaultUruncConfig())
		assert.Error(t, err, "Expected New to fail for an invalid annotation value")
		assert.NotErrorIs(t, err, ErrNotUnikernel, "Expected a specific error instead of ErrNotUnikernel")
		assert.ErrorContains(t, err, annotMountRootfs, "Expected error to mention the annotation")
	})

	t.Run("valid annotations reach the container state", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		annots[annotMountRootfs] = "TRUE"

		u, err := New(writeBundle(t, annots), "test-container", t.TempDir(), defaultUruncConfig())
		assert.NoError(t, err, "Expected New to succeed")
		// The state also holds the urunc_config.* entries, so check only that
		// every annotation reached it unchanged.
		for key, val := range annots {
			assert.Equal(t, val, u.State.Annotations[key],
				"Expected %s to reach the container state unchanged", key)
		}
	})

	// vAccel is a host feature. Unless the urunc configuration enables it, a
	// container asking for it must not get created.
	t.Run("vAccel annotations are refused when vAccel is disabled", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		annots[annotVAccel] = "vsock"
		annots[annotRPCAddress] = "vsock://2:2049"

		_, err := New(writeBundle(t, annots), "test-container", t.TempDir(), defaultUruncConfig())
		assert.Error(t, err, "Expected New to fail when vAccel is disabled")
		assert.NotErrorIs(t, err, ErrNotUnikernel, "Expected a specific error instead of ErrNotUnikernel")
		assert.ErrorContains(t, err, annotVAccel, "Expected error to mention the annotation")
	})

	t.Run("vAccel annotations are accepted when vAccel is enabled", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		annots[annotVAccel] = "vsock"
		annots[annotRPCAddress] = "vsock://2:2049"
		cfg := defaultUruncConfig()
		cfg.Runtime.VAccel = true

		u, err := New(writeBundle(t, annots), "test-container", t.TempDir(), cfg)
		assert.NoError(t, err, "Expected New to succeed when vAccel is enabled")
		assert.Equal(t, "vsock://2:2049", u.State.Annotations[annotRPCAddress])
	})

	// A container without any urunc annotation is not a unikernel one and urunc
	// needs to hand it over to runc.
	t.Run("missing annotations fall back to runc", func(t *testing.T) {
		t.Parallel()
		_, err := New(writeBundle(t, map[string]string{}), "test-container", t.TempDir(), defaultUruncConfig())
		assert.ErrorIs(t, err, ErrNotUnikernel, "Expected ErrNotUnikernel for a plain container")
	})
}
