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

// Platform-neutral bundle handling shared by the Linux orchestration engine
// and the darwin runner: OCI spec loading, the bundle filename constants, and
// the package logger. Parsing an OCI bundle is identical on every platform,
// so it lives here rather than being duplicated per platform.

package unikontainers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"

	"github.com/urunc-dev/urunc/internal/constants"
)

var uniklog = logrus.WithField("subsystem", "unikontainers")

const (
	configFilename    = "config.json"
	stateFilename     = "state.json"
	initPidFilename   = "init.pid"
	uruncJSONFilename = "urunc.json"
	rootfsDirName     = "rootfs"

	// monitorRootfsDirName is the bundle subdirectory where the monitor's
	// execution rootfs is staged. Shared because ChooseRootfs/switchMonRootfs
	// (platform-neutral) reference it.
	monitorRootfsDirName = constants.MonitorRootfsDirName
)

// LoadSpec reads and parses the OCI runtime specification (config.json) from a
// bundle directory. Exported so platform runners outside this package (the
// darwin cmd/urunc path) parse the bundle through the same code as the Linux
// orchestration engine.
func LoadSpec(bundleDir string) (*specs.Spec, error) {
	return loadSpec(bundleDir)
}

// loadSpec reads and parses the OCI runtime specification (config.json) from a
// bundle directory.
func loadSpec(bundleDir string) (*specs.Spec, error) {
	var spec specs.Spec

	absBundleDir, err := filepath.Abs(bundleDir)
	if err != nil {
		return nil, fmt.Errorf("failed to find absolute path of bundle: %w", err)
	}

	configFile := filepath.Join(absBundleDir, configFilename)
	specData, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read specification file: %w", err)
	}

	if err := json.Unmarshal(specData, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse specification json: %w", err)
	}

	return &spec, nil
}
