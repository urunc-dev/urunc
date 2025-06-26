// Copyright (c) 2023-2025, Nubificus LTD
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

package hypervisors

import (
	"fmt"

	hedge "github.com/nubificus/hedge_cli/hedge_api"
	"github.com/nubificus/urunc/pkg/unikontainers/unikernels"
)

const (
	HedgeVmm         VmmType = "hedge"
	maxVMListRetries int     = 20
	ConsoleEndpoint          = "/proc/vmcons"
)

type Hedge struct{}

func (h *Hedge) Ok() error {
	return fmt.Errorf("hedge not implemented yet")
}

func (h *Hedge) Stop(_ string) error {
	return fmt.Errorf("hedge not implemented yet")
}

func (h *Hedge) UsesKVM() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (h *Hedge) SupportsSharedfs() bool {
	return false
}

func (h *Hedge) Path() string {
	return ""
}

func (h *Hedge) Execve(_ ExecArgs, _ unikernels.Unikernel) error {
	return fmt.Errorf("hedge not implemented yet")
}

func (h *Hedge) VMState(name string) string {
	vms, err := hedge.ListVMs()
	if err != nil {
		return "error"
	}
	for _, vm := range vms {
		if vm.Name == name {
			return "running"
		}
	}
	return "unknown"
}
