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

package hypervisors

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/sirupsen/logrus"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const DefaultMemory uint64 = 256 // The default memory for every hypervisor: 256 MB

type VmmType string

// Monitor type names of the Linux-only backends. They live here, not next to
// the backend, so platform-neutral code (annotation validation) can name a
// monitor without pulling in the backend that implements it.
const (
	HvtVmm         VmmType = "hvt"
	FirecrackerVmm VmmType = "firecracker"
)

var ErrVMMNotInstalled = errors.New("vmm not found")
var vmmLog = logrus.WithField("subsystem", "monitors")

// VMMFactory describes how to construct one monitor. The registry
// (`vmmFactories`) that maps VmmType -> VMMFactory is platform-specific
// (Linux monitors vs the macOS QEMU/Vz set), but the NewVMM driver that
// consumes it is shared. precheck and pathFunc are optional per-monitor hooks
// for platform quirks (e.g. the darwin HVF check and QEMU binary discovery)
// so the driver stays platform-neutral.
type VMMFactory struct {
	binary     string
	createFunc func(binary, binaryPath string, vhost bool) types.VMM
	precheck   func() error                                                  // optional availability gate
	pathFunc   func(monitors map[string]types.MonitorConfig) (string, error) // optional custom binary resolution
}

// NewVMM constructs a monitor of the requested type from the platform's
// registry. Shared across Linux and darwin; only the `vmmFactories` map and
// the optional hooks differ per platform.
func NewVMM(vmmType VmmType, monitors map[string]types.MonitorConfig) (vmm types.VMM, err error) {
	defer func() {
		if err != nil {
			vmmLog.Error(err.Error())
		}
	}()

	factory, exists := vmmFactories[vmmType]
	if !exists {
		return nil, fmt.Errorf("vmm \"%s\" is not supported", vmmType)
	}

	if factory.precheck != nil {
		if err := factory.precheck(); err != nil {
			return nil, err
		}
	}

	var vmmPath string
	if factory.binary != "" {
		if factory.pathFunc != nil {
			vmmPath, err = factory.pathFunc(monitors)
		} else {
			vmmPath, err = getVMMPath(vmmType, factory.binary, monitors)
		}
		if err != nil {
			return nil, err
		}
	}

	return factory.createFunc(factory.binary, vmmPath, monitors[string(vmmType)].Vhost), nil
}

func getVMMPath(vmmType VmmType, binary string, monitors map[string]types.MonitorConfig) (string, error) {
	if vmmPath := monitors[string(vmmType)].BinaryPath; vmmPath != "" {
		return vmmPath, nil
	}

	lookupBinary := binary
	if vmmType == QemuVmm {
		lookupBinary = binary + cpuArch()
	}

	vmmPath, err := exec.LookPath(lookupBinary)
	if err != nil {
		return "", ErrVMMNotInstalled
	}
	return vmmPath, nil
}
