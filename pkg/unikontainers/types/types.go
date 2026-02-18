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

//revive:disable:var-naming
package types

type Unikernel interface {
	Init(UnikernelParams) error
	CommandString() (string, error)
	SupportsBlock() bool
	SupportsFS(string) bool
	MonitorNetCli(string, string) string
	MonitorBlockCli() []MonitorBlockArgs
	MonitorCli() MonitorCliArgs
}

type VMM interface {
	// BuildExecCmd builds and validates the VMM command arguments without executing.
	// This is used to verify the command can be built before reporting container as started.
	// The returned slice contains the command path as the first element followed by arguments.
	BuildExecCmd(args ExecArgs, ukernel Unikernel) ([]string, error)
	// PreExec performs any monitor-specific setup that must happen after BuildExecCmd
	// succeeds but before syscall.Exec is called. For example, HVT applies seccomp
	// filters here. Most monitors can return nil (no-op).
	PreExec(args ExecArgs) error
	Stop(int) error
	Path() string
	UsesKVM() bool
	SupportsSharedfs(string) bool
	Ok() error
}

type NetDevParams struct {
	IP      string // The veth device IP
	Mask    string // The veth device mask
	Gateway string // The veth device gateway
	MAC     string // The MAC address of the guest network device
	TapDev  string // The tap device name
}

type BlockDevParams struct {
	Source     string
	MountPoint string
	FsType     string
	ID         string
}

type SharedfsParams struct {
	Type string // The type of shared-fs 9p or virtiofs
	Path string // The path in the host to share with guest
}

type RootfsParams struct {
	Type        string // The type of rootfs (block, initrd, 9pfs, virtiofs)
	Path        string // The path in the host where rootfs resides
	MountedPath string // The mountpoint in the host where the rootfs is mounted
	MonRootfs   string // The rootfs for the monitor process
}

// Specific to Linux
type ProcessConfig struct {
	UID     uint32 // The uid of the process inside the guest
	GID     uint32 // The gid of the process inside the guest
	WorkDir string // The workdir of the process inside the guest
}

// UnikernelParams holds the data required to build the unikernels commandline
type UnikernelParams struct {
	CmdLine    []string // The cmdline provided by the image
	EnvVars    []string // The environment variables provided by the image
	Monitor    string   // The monitor where guest will execute
	Version    string   // The version of the unikernel
	InitrdPath string   // The path to the initrd of the unikernel
	Net        NetDevParams
	Block      []BlockDevParams
	Rootfs     RootfsParams  // Information about rootfs
	ProcConf   ProcessConfig // Information for the process execution inside the guest
}

// ExecArgs holds the data required by Execve to start the VMM
// FIXME: add extra fields if required by additional VMM's
type ExecArgs struct {
	ContainerID   string   // The container ID
	Environment   []string // The environment variables of the monitor
	Command       string   // The unikernel's command line
	Seccomp       bool     // Enable or disable seccomp filters for the VMM
	MemSizeB      uint64   // The size of the memory provided to the VM in bytes
	VCPUs         uint     // The number of vCPUs to allocate
	UnikernelPath string   // The path of the unikernel inside rootfs
	InitrdPath    string   // The path to the initrd of the unikernel
	VAccelType    string   // Specifies the vAccel acceleration type(e.g. vsock). When empty, vAccel is disabled
	VSockDevPath  string   // The host directory where the fc unix socket is created
	VSockDevID    int      // The guest-cid
	Net           NetDevParams
	Sharedfs      SharedfsParams
}

type MonitorCliArgs struct {
	ExtraInitrd string
	OtherArgs   string
}

type MonitorBlockArgs struct {
	ID        string
	Path      string
	ExactArgs string
}

// ExtraBinConfig struct is used to hold specific configuration for extra binaries
// like virtiofsd. It is parsed from the urunc config file or state.json annotations
type ExtraBinConfig struct {
	Path    string `toml:"path"`              // The path to the binary
	Options string `toml:"options,omitempty"` // Optional cli options for the extra binary
}

// MonitorConfig struct is used to hold hypervisor specific configuration
// that is parsed from the urunc config file or state.json annotations
type MonitorConfig struct {
	DefaultMemoryMB uint   `toml:"default_memory_mb"`
	DefaultVCPUs    uint   `toml:"default_vcpus"`
	BinaryPath      string `toml:"path,omitempty"`      // Optional path to the hypervisor binary
	DataPath        string `toml:"data_path,omitempty"` // Optional path to the hypervisor data files (e.g. qemu bios stuff)
	Vhost           bool   `toml:"vhost,omitempty"`     // Optional: enable vhost for network performance optimization
}
