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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

var errGuestRootfsChoiceSkipped = errors.New("guest rootfs choice skipped")

// chooseGuestRootfs runs the same ChooseRootfs logic as runtime Exec after inner
// task Create (#684). The caller persists the result in bundle config.json so
// Exec can reuse the selection.
func chooseGuestRootfs(r *taskAPI.CreateTaskRequest) (types.RootfsParams, error) {
	configPath := filepath.Join(r.Bundle, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return types.RootfsParams{}, fmt.Errorf("read config.json: %w", err)
	}

	var spec specs.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return types.RootfsParams{}, fmt.Errorf("unmarshal config.json: %w", err)
	}
	if spec.Root == nil {
		return types.RootfsParams{}, fmt.Errorf("invalid OCI spec: root section is required")
	}

	config, err := unikontainers.GetUnikernelConfig(filepath.Clean(r.Bundle), &spec)
	if err != nil {
		return types.RootfsParams{}, fmt.Errorf("%w: %w", errGuestRootfsChoiceSkipped, err)
	}

	annotations := config.Map()
	uruncCfg, err := unikontainers.LoadUruncConfig(unikontainers.UruncConfigPath)
	if err != nil && uruncCfg == nil {
		return types.RootfsParams{}, err
	}

	rootfsParams, err := unikontainers.ChooseRootfs(
		filepath.Clean(r.Bundle),
		spec.Root.Path,
		annotations,
		uruncCfg,
	)
	if err != nil {
		return types.RootfsParams{}, err
	}

	return rootfsParams, nil
}
