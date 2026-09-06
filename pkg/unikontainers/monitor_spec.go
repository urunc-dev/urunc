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

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// monitorSpecFilename is the file that contains the information to finalize the
// setup of the monitor's execution environment. It is written into the monitor rootfs
// in order to let the process that libcontainer spawns to read it.
const monitorSpecFilename = ".monitor_spec.json"

// monitorSpec contains the information for the urunc reexec process to
// finalize the monitor's process execution environment and exec the monitor.
type monitorSpec struct {
	ContainerID   string                `json:"containerID"`
	UnikernelType string                `json:"unikernelType"`
	MonitorType   string                `json:"monitorType"`
	MonitorCfg    types.MonitorConfig   `json:"monitorCfg"`
	ExecArgs      types.ExecArgs        `json:"execArgs"`
	GuestParams   types.UnikernelParams `json:"guestParams"`
	NetworkType   string                `json:"networkType"`
	User          specs.User            `json:"user"`
	PreStartCmd   []string              `json:"preStartCmd,omitempty"`
}

// writeMonitorSpec builds the monitor spec and writes it into the monitor rootfs.
func (u *Unikontainer) writeMonitorSpec(rootfsParams types.RootfsParams, monRes monitorResources) error {
	mSpec := u.buildMonitorSpec(rootfsParams, monRes)

	mSpec.NetworkType = u.getNetworkType()
	mSpec.User = u.Spec.Process.User

	// The post-pivot process sees the monitor rootfs as "/", so the guest rootfs
	// path it is handed has to be relative to it.
	mSpec.GuestParams.Rootfs.MonRootfs = "/"

	// The monitor's environment is not persisted: the urunc monitor process
	// inherits it from libcontainer anyway, and writing the host's environment
	// into a file inside the monitor rootfs is exposure with no upside.
	mSpec.ExecArgs.Environment = nil

	data, err := json.Marshal(mSpec)
	if err != nil {
		return fmt.Errorf("could not encode the monitor spec: %w", err)
	}

	path, err := securejoin.SecureJoin(rootfsParams.MonRootfs, monitorSpecFilename)
	if err != nil {
		return fmt.Errorf("could not resolve path for monitor spec: %w", err)
	}

	err = os.WriteFile(path, data, 0o600)
	if err != nil {
		return fmt.Errorf("could not write the monitor spec: %w", err)
	}

	return nil
}

// LoadMonitorSpec reads the monitor spec-file from dir
func LoadMonitorSpec(dir string) (monitorSpec, error) {
	var ms monitorSpec

	data, err := os.ReadFile(filepath.Join(dir, monitorSpecFilename))
	if err != nil {
		return ms, err
	}

	err = json.Unmarshal(data, &ms)
	if err != nil {
		return ms, fmt.Errorf("could not decode the monitor spec: %w", err)
	}

	return ms, nil
}

// RemoveMonitorSpec deletes the monitor spec file from dir.
func RemoveMonitorSpec(dir string) error {
	path, err := securejoin.SecureJoin(dir, monitorSpecFilename)
	if err != nil {
		return fmt.Errorf("could not resolve path for monitor spec: %w", err)
	}
	return os.Remove(path)
}
