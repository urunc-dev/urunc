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

package types

type Unikernel interface {
	Init(UnikernelParams) error
	CommandString() (string, error)
	SupportsBlock() bool
	SupportsFS(string) bool
	MonitorNetCli(string, string, string) string
	MonitorBlockCli(string) string
	MonitorCli(string) string
}

// UnikernelParams holds the data required to build the unikernels commandline
type UnikernelParams struct {
	CmdLine          []string // The cmdline provided by the image
	EnvVars          []string // The environment variables provided by the image
	EthDeviceIP      string   // The eth device IP
	EthDeviceMask    string   // The eth device mask
	EthDeviceGateway string   // The eth device gateway
	RootFSType       string   // The rootfs type of the Unikernel
	BlockMntPoint    string   // The mount point for the block device
	Version          string   // The version of the unikernel
}

// HypervisorConfig struct is used to hold hypervisor specific configuration
// that is parsed from the urunc config file or state.json annotations
type HypervisorConfig struct {
	DefaultMemoryMB uint   `toml:"default_memory_mb"`
	DefaultVCPUs    uint   `toml:"default_vcpus"`
	BinaryPath      string `toml:"binary_path,omitempty"` // Optional path to the hypervisor binary
}

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

type VMM interface {
	Execve(args ExecArgs, ukernel Unikernel) error
	Stop(t string) error
	Path() string
	UsesKVM() bool
	SupportsSharedfs(string) bool
	Ok() error
}
