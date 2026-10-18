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
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
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

// What urunc does with an image whose annotations name a type and a monitor
// but no binary must not change for the monitors that need one: such a
// container is not a unikernel one and goes to runc, as before.
func TestNewWithoutBinary(t *testing.T) {
	t.Parallel()

	t.Run("qemu without a binary still falls back to runc", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		delete(annots, annotBinary)

		_, err := New(writeBundle(t, annots), "test-container", t.TempDir(), defaultUruncConfig())
		assert.ErrorIs(t, err, ErrNotUnikernel, "Expected ErrNotUnikernel without a binary on qemu")
	})

	t.Run("hyperlight-unikraft without a binary is a unikernel container", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		delete(annots, annotBinary)
		annots[annotHypervisor] = string(hypervisors.HyperlightVmm)
		annots[annotInitrd] = "/unikernel/initrd.cpio"

		u, err := New(writeBundle(t, annots), "test-container", t.TempDir(), defaultUruncConfig())
		assert.NoError(t, err, "Expected New to succeed without a binary on hyperlight-unikraft")
		assert.Equal(t, "", u.State.Annotations[annotBinary])
	})

	// urunc.json stores its values base64-encoded, so the monitor is only
	// recognizable once decoded: the same two outcomes must hold there.
	t.Run("qemu without a binary in urunc.json still falls back to runc", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		delete(annots, annotBinary)

		_, err := New(writeBundleWithUruncJSON(t, annots), "test-container", t.TempDir(), defaultUruncConfig())
		assert.ErrorIs(t, err, ErrNotUnikernel, "Expected ErrNotUnikernel without a binary on qemu")
	})

	t.Run("hyperlight-unikraft without a binary in urunc.json is a unikernel container", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		delete(annots, annotBinary)
		annots[annotHypervisor] = string(hypervisors.HyperlightVmm)
		annots[annotInitrd] = "/unikernel/initrd.cpio"

		u, err := New(writeBundleWithUruncJSON(t, annots), "test-container", t.TempDir(), defaultUruncConfig())
		assert.NoError(t, err, "Expected New to succeed without a binary on hyperlight-unikraft")
		assert.Equal(t, string(hypervisors.HyperlightVmm), u.State.Annotations[annotHypervisor])
		assert.Equal(t, "", u.State.Annotations[annotBinary])
	})
}

// writeBundleWithUruncJSON writes a bundle whose spec carries no urunc
// annotations and whose rootfs holds them in urunc.json, base64-encoded as
// the packaging tools write it.
func writeBundleWithUruncJSON(t *testing.T, annots map[string]string) string {
	t.Helper()

	bundle := writeBundle(t, map[string]string{})
	encoded := make(map[string]string, len(annots))
	for k, v := range annots {
		encoded[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	data, err := json.Marshal(encoded)
	assert.NoError(t, err)
	rootfs := filepath.Join(bundle, rootfsDirName)
	assert.NoError(t, os.MkdirAll(rootfs, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(rootfs, uruncJSONFilename), data, 0o600))

	return bundle
}
