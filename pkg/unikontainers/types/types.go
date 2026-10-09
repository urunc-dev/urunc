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

import "golang.org/x/sys/unix"

// ResolvConfPath is where the resolv.conf of the container is mounted in the
// monitor rootfs, for a monitor that installs it in the guest.
const ResolvConfPath = "/etc/resolv.conf"

type Unikernel interface {
	Init(UnikernelParams) error
	CommandString() (string, error)
	SupportsBlock() bool
	SupportsFS(string) bool
	MonitorNetCli(string, string) []string
	MonitorBlockCli() []MonitorBlockArgs
	MonitorSharedfsCli(string, string) []string
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
	Signal(int, unix.Signal) error
	Path() string
	UsesKVM() bool
	// UsesTUNTAP reports whether the guest gets its network through a tap
	// device, which urunc creates in the container's network namespace and
	// redirects the traffic of the container's interface to. A monitor that
	// opens the guest's sockets itself, in that namespace, reports false, and
	// urunc leaves the interface untouched: redirecting its traffic to a tap
	// would take it away from those sockets.
	UsesTUNTAP() bool
	SupportsSharedfs(string) bool
	SupportsControlSocket() bool
	Ok() error
}

// NetDevParams describes the network of the container: its interface, and
// the tap device that the traffic of the interface is redirected to when the
// monitor uses one. A container started with no network leaves it empty.
type NetDevParams struct {
	IP        string // The veth device IP
	Mask      string // The veth device mask
	Gateway   string // The veth device gateway
	MAC       string // The MAC address of the guest network device
	TapDev    string // The tap device name
	MTU       int    // The MTU value of the tap device
	DNSServer string // The nameserver of the container, empty if there is none
	// UnixSocket is a host unix socket that carries the guest's network
	// traffic on darwin (a QEMU stream netdev, or hvi's --net-gateway).
	UnixSocket string
}

type BlockDevParams struct {
	Source         string
	MountPoint     string
	FsType         string
	ID             string
	HostMountPoint string
	LoopAutoclear  bool
	MountOptions   string
	// IsExplicit marks a Source that is a path inside the container's image
	// rootfs (an explicit block image)
	IsExplicit bool
}

type SharedfsParams struct {
	Type     string // The type of shared-fs 9p or virtiofs
	Path     string // The path in the host to share with guest
	Tag      string // The guest mount tag; empty means the backend default
	ReadOnly bool   // Export the share read-only
}

// SharedDirParams describes an extra host directory that a darwin backend
// exports to the guest next to the rootfs share.
type SharedDirParams struct {
	Path     string // The host directory
	Tag      string // The guest mount tag
	ReadOnly bool   // Export the directory read-only
}

type RootfsParams struct {
	Type        string // The type of rootfs (block, initrd, 9pfs, virtiofs)
	Path        string // The path in the host where rootfs resides
	MountedPath string // The mountpoint in the host where the rootfs is mounted
	MonRootfs   string // The rootfs for the monitor process
	// FsType is the filesystem of a block rootfs (e.g. ext2, ext4). A generic
	// container boot on a block device needs it on the kernel command line so the
	// boot initrd's /init can mount /dev/vda; other rootfs types leave it empty.
	FsType string
}

// Specific to Linux
type ProcessConfig struct {
	UID     uint32 // The uid of the process inside the guest
	GID     uint32 // The gid of the process inside the guest
	WorkDir string // The workdir of the process inside the guest
}

// UnikernelParams holds the data required by the unikernels commandline
type UnikernelParams struct {
	CmdLine    []string // The cmdline provided by the image
	EnvVars    []string // The environment variables provided by the image
	Monitor    string   // The monitor where guest will execute
	Version    string   // The version of the unikernel
	InitrdPath string   // The path to the initrd of the unikernel
	NetDevName string   // The name of the guest network device declared at build time
	BlkDevName string   // The name of the guest block device declared at build time
	Net        NetDevParams
	Block      []BlockDevParams
	Rootfs     RootfsParams  // Information about rootfs
	ProcConf   ProcessConfig // Information for the process execution inside the guest
	// ContainerBoot marks a generic container boot: an unmodified OCI image
	// booted with a host kernel and boot initrd, whose /init mounts the shared
	// container rootfs and hands over to urunit. InitrdPath then names the
	// boot initrd inside the monitor rootfs.
	ContainerBoot bool
	// Verbose asks the guest to be more talkative on its console, mirroring
	// urunc's own log verbosity (the [log] level in the config). A Linux guest
	// boots quietly by default and only logs the full kernel messages when this
	// is set.
	Verbose bool
}

// BlockDevSpec describes one disk image attached to the guest as virtio-blk.
type BlockDevSpec struct {
	Path     string // The path to the disk image on the host
	ReadOnly bool   // Attach the disk read-only
}

// ExecArgs holds the data required by Execve to start the VMM
// FIXME: add extra fields if required by additional VMM's
type ExecArgs struct {
	ContainerID   string   // The container ID
	Environment   []string // The environment variables of the monitor
	Command       string   // The unikernel's command line
	Seccomp       bool     // Enable or disable seccomp filters for the VMM
	MemSizeB      uint64   // The size of the memory provided to the VM in bytes
	ResolvConf    bool     // The container's resolv.conf is mounted at ResolvConfPath in the monitor rootfs, for a monitor that installs it in the guest
	Ports         string   // The ports the guest may serve on, as the annotation of the monitor lists them; "" for any
	VCPUs         uint     // The number of vCPUs to allocate
	UnikernelPath string   // The path of the unikernel inside rootfs
	InitrdPath    string   // The path to the initrd of the unikernel
	SnapshotPath  string   // The directory of a saved hluk snapshot inside the image; empty boots the kernel and initrd
	AgentVsockCID int      // Guest CID of the vhost-vsock device for the in-guest exec agent; 0 disables it
	AgentVsockUDS string   // firecracker, cloud-hypervisor and hvi: monitor-rootfs path of the agent's vsock unix socket (qemu uses host vhost-vsock instead)
	VAccelType    string   // Specifies the vAccel acceleration type(e.g. vsock). When empty, vAccel is disabled
	VSockDevPath  string   // The directory inside the monitor rootfs with the vAccel unix sockets
	VSockDevID    int      // The guest-cid
	SocketPath    string   // The path of the monitor's control socket (empty means no control socket)
	Net           NetDevParams
	Sharedfs      SharedfsParams

	// hull fills the fields below and the darwin backends read them. The
	// Linux backends ignore them, so their zero values change nothing there.

	KernelPath         string            // The kernel image, when it is not UnikernelPath
	RootfsPath         string            // A directory or file rootfs for the guest
	BlockDevPath       string            // A raw image attached as virtio-blk
	LogFile            string            // The file the monitor writes its log to
	AgentSockPath      string            // The host unix socket the VMM bridges to the guest agent
	VirtiofsSocketPath string            // The vhost-user socket of an external virtiofsd
	SharedDirs         []SharedDirParams // Extra host directories exported to the guest
	// BlockDevs are the disks to attach, in the order they go on the VMM's
	// command line. The guest finds each one by its serial, disk<N>, where N
	// is its position here, so the order matters. BlockDevPath is the older
	// single-disk form; it counts as one writable disk when BlockDevs is
	// empty, and is ignored otherwise. Use EffectiveBlockDevs to read both.
	// Read by the darwin builders only (hvi, Vz).
	BlockDevs []BlockDevSpec
	GuestUID  uint32 // The uid that owns the exported files in the guest
	GuestGID  uint32 // The gid that owns the exported files in the guest
	GUI       bool   // Open a window for the guest display
	GUITitle  string // The title of that window
}

// EffectiveBlockDevs returns the disks to attach, in order: BlockDevs when
// it is set, else BlockDevPath as a single writable disk, else nil.
func (a ExecArgs) EffectiveBlockDevs() []BlockDevSpec {
	if len(a.BlockDevs) > 0 {
		return a.BlockDevs
	}
	if a.BlockDevPath != "" {
		return []BlockDevSpec{{Path: a.BlockDevPath}}
	}
	return nil
}

type MonitorCliArgs struct {
	ExtraInitrd string
	OtherArgs   []string
}

type MonitorBlockArgs struct {
	ID        string
	Path      string
	ExactArgs []string
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
	BinaryPath      string `toml:"path,omitempty"`        // Optional path to the hypervisor binary
	DataPath        string `toml:"data_path,omitempty"`   // Optional path to the hypervisor data files (e.g. qemu bios stuff)
	Vhost           bool   `toml:"vhost,omitempty"`       // Optional: enable vhost for network performance optimization
	SocketPath      string `toml:"socket_path,omitempty"` // Optional path for the monitor's control socket (unset means no control socket)
}
