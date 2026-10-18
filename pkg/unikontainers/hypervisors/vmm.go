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

var ErrVMMNotInstalled = errors.New("vmm not found")
var vmmLog = logrus.WithField("subsystem", "monitors")

// The monitors below are named here because the Linux and the darwin
// registries both refer to them.
const (
	HvtVmm         VmmType = "hvt"
	FirecrackerVmm VmmType = "firecracker"
	HviVmm         VmmType = "hvi"
	HviBinary      string  = "hvi"
)

// VMMFactory describes how NewVMM builds one monitor type.
type VMMFactory struct {
	// binary is the name looked up in PATH. An empty binary means the
	// monitor has no executable and NewVMM resolves no path for it.
	binary     string
	createFunc func(binary, binaryPath string, vhost bool) types.VMM
	// precheck, when set, runs first. Its error is returned as is.
	precheck func() error
	// pathFunc, when set, resolves the binary path instead of getVMMPath.
	pathFunc func(monitors map[string]types.MonitorConfig) (string, error)
}

// NewVMM builds the monitor vmmType from the registry of the platform urunc
// runs on.
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

	vmmPath := ""
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
