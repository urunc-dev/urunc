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
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

// WriteRootfsViewState persists shim-prepared rootfs view state in the bundle.
func WriteRootfsViewState(bundleDir string, state types.RootfsViewState) error {
	bundleDir = filepath.Clean(bundleDir)
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", rootfsViewFilename, err)
	}
	path := filepath.Join(bundleDir, rootfsViewFilename)
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // bundle metadata, same as state.json
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// LoadRootfsViewState reads rootfs view state written by the shim at task Create.
// Returns (nil, nil) when the file is absent.
func LoadRootfsViewState(bundleDir string) (*types.RootfsViewState, error) {
	bundleDir = filepath.Clean(bundleDir)
	path := filepath.Join(bundleDir, rootfsViewFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var state types.RootfsViewState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return &state, nil
}

func rootfsViewBootArtifactBindPaths(viewRoot, monRootfs, unikernelPath, initrdPath, uruncJSON string) []struct{ src, target string } {
	artifactPaths := []string{unikernelPath, uruncJSON}
	if initrdPath != "" {
		artifactPaths = append(artifactPaths, initrdPath)
	}
	files := make([]struct{ src, target string }, 0, len(artifactPaths))
	for _, artifactPath := range artifactPaths {
		rootfsRelPath := strings.TrimPrefix(filepath.Clean(artifactPath), "/")
		files = append(files, struct{ src, target string }{
			src:    filepath.Join(viewRoot, rootfsRelPath),
			target: filepath.Join(monRootfs, rootfsRelPath),
		})
	}
	return files
}

func rollbackRootfsViewBinds(targets []string) {
	for i := len(targets) - 1; i >= 0; i-- {
		if err := unmountRootfsViewBind(targets[i]); err != nil {
			uniklog.WithError(err).WithField("target", filepath.Clean(targets[i])).Warn("failed to roll back rootfs view bind mount")
		}
	}
}

// probeRootfsViewBootArtifacts checks that boot artifacts can be bind-mounted
// from the shim-mounted view. preSetup still has mountedPath; binds are rolled
// back immediately.
func probeRootfsViewBootArtifacts(view *types.RootfsViewState, unikernelPath, initrdPath, uruncJSON string) (useView bool, err error) {
	if view == nil || view.Mountpoint == "" {
		return false, nil
	}

	probeRoot, err := os.MkdirTemp("", "urunc-rootfs-view-probe-")
	if err != nil {
		return false, fmt.Errorf("create temporary rootfs view probe mountpoint: %w", err)
	}
	defer os.RemoveAll(probeRoot)

	// Probe binds only validate the source view; monitor binds are created later.
	var probeBindTargets []string
	defer func() {
		rollbackRootfsViewBinds(probeBindTargets)
	}()

	for _, f := range rootfsViewBootArtifactBindPaths(view.Mountpoint, probeRoot, unikernelPath, initrdPath, uruncJSON) {
		dstPath := f.target
		if err := bindMountFile(f.src, filepath.Dir(dstPath), dstPath, 0, unix.MS_BIND, false); err != nil {
			return false, fmt.Errorf("bind view %s -> %s: %w", f.src, f.target, err)
		}
		probeBindTargets = append(probeBindTargets, dstPath)
	}

	return true, nil
}

// prepareRootfsViewBootBinds runs after prepareRoot. The source view mount is
// owned by the shim and remains mounted until task cleanup.
func prepareRootfsViewBootBinds(view *types.RootfsViewState, monRootfs, unikernelPath, initrdPath, uruncJSON string) error {
	if view == nil || view.Mountpoint == "" {
		return nil
	}

	var bindTargets []string
	keepBinds := false
	defer func() {
		if !keepBinds {
			rollbackRootfsViewBinds(bindTargets)
		}
	}()

	for _, f := range rootfsViewBootArtifactBindPaths(view.Mountpoint, monRootfs, unikernelPath, initrdPath, uruncJSON) {
		dstPath := f.target
		if err := bindMountFile(f.src, filepath.Dir(dstPath), dstPath, 0, unix.MS_BIND, false); err != nil {
			return fmt.Errorf("bind view %s -> %s: %w", f.src, f.target, err)
		}
		bindTargets = append(bindTargets, dstPath)
	}

	keepBinds = true
	return nil
}

func unmountRootfsViewBind(target string) error {
	target = filepath.Clean(target)
	err := unix.Unmount(target, unix.MNT_DETACH)
	if err == nil || err == unix.EINVAL || err == unix.ENOENT || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("failed to unmount rootfs view bind %s: %w", target, err)
}
