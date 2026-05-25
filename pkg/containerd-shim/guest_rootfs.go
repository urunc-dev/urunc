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
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	specs "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/internal/constants"
	containerdShim "github.com/urunc-dev/urunc/pkg/containerd-shim/containerd"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
)

var errGuestRootfsChoiceSkipped = errors.New("guest rootfs choice skipped")

// chooseGuestRootfs runs the same ChooseRootfs logic as runtime Exec after inner
// task Create and records the result in constants.AnnotRootfsParams so Exec knows
// selection already happened.
func chooseGuestRootfs(r *taskAPI.CreateTaskRequest) error {
	return containerdShim.PatchConfig(r.Bundle, func(spec *specs.Spec) error {
		if spec.Root == nil {
			return fmt.Errorf("invalid OCI spec: root section is required")
		}

		config, err := unikontainers.GetUnikernelConfig(filepath.Clean(r.Bundle), spec)
		if err != nil {
			return fmt.Errorf("%w: %w", errGuestRootfsChoiceSkipped, err)
		}

		annotations := config.Map()
		uruncCfg, err := unikontainers.LoadUruncConfig(unikontainers.UruncConfigPath)
		if err != nil && uruncCfg == nil {
			return err
		}

		rootfsParams, err := unikontainers.ChooseRootfs(
			filepath.Clean(r.Bundle),
			spec.Root.Path,
			annotations,
			uruncCfg,
		)
		if err != nil {
			return err
		}

		encoded, err := json.Marshal(rootfsParams)
		if err != nil {
			return err
		}

		if spec.Annotations == nil {
			spec.Annotations = make(map[string]string)
		}
		spec.Annotations[constants.AnnotRootfsParams] = string(encoded)
		return nil
	})
}
