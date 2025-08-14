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
	"errors"
	"fmt"
	"os/exec"

	"github.com/sirupsen/logrus"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// we declare HypervisorConfig struct here to avoid import cycles

// HypervisorConfig struct is used to hold hypervisor specific configuration
// that is parsed from the urunc config file or state.json annotations
type HypervisorConfig struct {
	DefaultMemoryMB uint   `toml:"default_memory_mb"`
	DefaultVCPUs    uint   `toml:"default_vcpus"`
	BinaryPath      string `toml:"binary_path,omitempty"` // Optional path to the hypervisor binary
}

const DefaultMemory uint64 = 256 // The default memory for every hypervisor: 256 MB

// ExecArgs holds the data required by Execve to start the VMM
// FIXME: add extra fields if required by additional VMM's
type ExecArgs struct {
	Container     string   // The container ID
	UnikernelPath string   // The path of the unikernel inside rootfs
	TapDevice     string   // The TAP device name
	BlockDevice   string   // The block device path
	InitrdPath    string   // The path to the initrd of the unikernel
	SharedfsType  string   // The type of shared-fs 9p or virtiofs
	SharedfsPath  string   // The path in the host to share with guest
	Command       string   // The unikernel's command line
	IPAddress     string   // The IP address of the TAP device
	GuestMAC      string   // The MAC address of the guest network device
	Seccomp       bool     // Enable or disable seccomp filters for the VMM
	MemSizeB      uint64   // The size of the memory provided to the VM in bytes
	VCPUs         uint     // The number of vCPUs to allocate
	Environment   []string // Environment
}

type VmmType string

var ErrVMMNotInstalled = errors.New("vmm not found")
var vmmLog = logrus.WithField("subsystem", "hypervisors")

type VMM interface {
	Execve(args ExecArgs, ukernel unikernels.Unikernel) error
	Stop(t string) error
	Path() string
	UsesKVM() bool
	SupportsSharedfs(string) bool
	Ok() error
}

type VMMFactory struct {
	binary     string
	createFunc func(binary, binaryPath string) VMM
}

var vmmFactories = map[VmmType]VMMFactory{
	SptVmm: {
		binary:     SptBinary,
		createFunc: func(binary, binaryPath string) VMM { return &SPT{binary: binary, binaryPath: binaryPath} },
	},
	HvtVmm: {
		binary:     HvtBinary,
		createFunc: func(binary, binaryPath string) VMM { return &HVT{binary: binary, binaryPath: binaryPath} },
	},
	QemuVmm: {
		binary:     QemuBinary,
		createFunc: func(binary, binaryPath string) VMM { return &Qemu{binary: binary, binaryPath: binaryPath} },
	},
	FirecrackerVmm: {
		binary:     FirecrackerBinary,
		createFunc: func(binary, binaryPath string) VMM { return &Firecracker{binary: binary, binaryPath: binaryPath} },
	},
}

func NewVMM(vmmType VmmType, hypervisors map[string]HypervisorConfig) (vmm VMM, err error) {
	defer func() {
		if err != nil {
			vmmLog.Error(err.Error())
		}
	}()

	// Handle Hedge separately since it is not in vmmFactories
	if vmmType == HedgeVmm {
		hedge := Hedge{}
		if err := hedge.Ok(); err != nil {
			return nil, ErrVMMNotInstalled
		}
		return &hedge, nil
	}

	factory, exists := vmmFactories[vmmType]
	if !exists {
		return nil, fmt.Errorf("vmm \"%s\" is not supported", vmmType)
	}

	vmmPath, err := getVMMPath(vmmType, factory.binary, hypervisors)
	if err != nil {
		return nil, err
	}

	return factory.createFunc(factory.binary, vmmPath), nil
}

func getVMMPath(vmmType VmmType, binary string, hypervisors map[string]HypervisorConfig) (string, error) {
	if vmmPath := hypervisors[string(vmmType)].BinaryPath; vmmPath != "" {
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
