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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/urunc-dev/urunc/pkg/network"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	m "github.com/urunc-dev/urunc/internal/metrics"
)

const (
	monitorRootfsDirName     string = "monRootfs"
	containerRootfsMountPath string = "/cntrRootfs"
	// libcontainerDirName is the directory under urunc's root used from libcontainer
	libcontainerDirName string = "libcontainer"
)

var uniklog = logrus.WithField("subsystem", "unikontainers")

var ErrQueueProxy = errors.New("this a queue proxy container")
var ErrNotUnikernel = errors.New("this is not a unikernel container")
var ErrNotExistingNS = errors.New("the namespace does not exist")

// Unikontainer holds the data necessary to create, manage and delete unikernel containers
type Unikontainer struct {
	State    *specs.State
	Spec     *specs.Spec
	BaseDir  string
	RootDir  string
	UruncCfg *UruncConfig
	Listener *net.UnixListener
	Conn     *net.UnixConn
	// readyPipe is the read end of the libcontainer-mode ready FIFO, held by
	// "urunc start" between opening it and reading the monitor's outcome.
	readyPipe *os.File
}

// New parses the bundle and creates a new Unikontainer object
func New(bundlePath string, containerID string, rootDir string, cfg *UruncConfig) (*Unikontainer, error) {
	spec, err := loadSpec(bundlePath)
	if err != nil {
		return nil, err
	}

	if spec == nil || spec.Linux == nil {
		return nil, fmt.Errorf("invalid OCI spec: linux section is required")
	}

	containerName := spec.Annotations["io.kubernetes.cri.container-name"]
	if containerName == "queue-proxy" {
		uniklog.Warn("This is a queue-proxy container. Adding IP env.")
		configFile := filepath.Join(bundlePath, configFilename)
		err = handleQueueProxy(*spec, configFile)
		if err != nil {
			return nil, err
		}
		return nil, ErrQueueProxy
	}

	config, err := GetUnikernelConfig(bundlePath, spec)
	if err != nil {
		return nil, ErrNotUnikernel
	}

	uniklog.Debugf("libcontainer runtime enabled: %t", cfg.Runtime.Libcontainer)

	confMap := config.Map()

	maps.Copy(confMap, cfg.Map())
	containerDir := filepath.Join(rootDir, containerID)
	state := &specs.State{
		Version:     spec.Version,
		ID:          containerID,
		Status:      "creating",
		Pid:         -1,
		Bundle:      bundlePath,
		Annotations: confMap,
	}
	return &Unikontainer{
		BaseDir:  containerDir,
		RootDir:  rootDir,
		Spec:     spec,
		State:    state,
		UruncCfg: cfg,
	}, nil
}

// Get retrieves unikernel data from disk to create a Unikontainer object
func Get(containerID string, rootDir string) (*Unikontainer, error) {
	u := &Unikontainer{}
	containerDir := filepath.Join(rootDir, containerID)
	stateFilePath := filepath.Join(containerDir, stateFilename)
	state, err := loadUnikontainerState(stateFilePath)
	if err != nil {
		return nil, err
	}
	if state.Annotations[annotType] == "" {
		return nil, ErrNotUnikernel
	}
	u.State = state

	spec, err := loadSpec(state.Bundle)
	if err != nil {
		return nil, err
	}
	if spec == nil || spec.Linux == nil {
		return nil, fmt.Errorf("invalid OCI spec: linux section is required")
	}
	u.BaseDir = containerDir
	u.RootDir = rootDir
	u.Spec = spec
	u.UruncCfg = UruncConfigFromMap(state.Annotations)
	uniklog.Debugf("libcontainer runtime enabled: %t", u.UruncCfg.Runtime.Libcontainer)
	return u, nil
}

// InitialSetup sets the Unikernel status as creating,
// creates the Unikernel base directory and
// saves the state.json file with the current Unikernel state
func (u *Unikontainer) InitialSetup() error {
	bundleDir := filepath.Clean(u.State.Bundle)
	rootfsDir := filepath.Clean(u.Spec.Root.Path)
	rootfsDir, err := resolveAgainstBase(bundleDir, rootfsDir)
	if err != nil {
		uniklog.Errorf("could not resolve rootfs directory %s: %v", rootfsDir, err)
		return err
	}

	unikernelPath := u.State.Annotations[annotBinary]
	initrdPath := u.State.Annotations[annotInitrd]
	unikernelType := u.State.Annotations[annotType]
	unikernel, err := unikernels.New(unikernelType)
	if err != nil {
		return err
	}

	// handle guest's rootfs.
	// There are four options:
	// 1. No rootfs for guest
	// 2. Use initrd or a block image inside the container's rootfs
	// 3. Use the devmapper snapshot as a block device for the guest's rootfs
	// 4. Use 9pfs to share the container's rootfs as the guest's rootfs
	// By default, urunc will not set any rootfs for the guest. However,
	// if the respective annotation is set then, depending on the guest
	// (supports block or 9pfs), it will use the supported option. In case
	// both ae supported, then the block option will be used by default.
	rootfsParams, err := ChooseRootfs(bundleDir, rootfsDir, u.State.Annotations, u.UruncCfg)
	if err != nil {
		uniklog.Errorf("could not choose guest rootfs: %v", err)
		return err
	}
	uniklog.WithFields(logrus.Fields{
		"rootfs_type": rootfsParams.Type,
		"rootfs_path": rootfsParams.Path,
		"mon_rootfs":  rootfsParams.MonRootfs,
		"mountedPath": rootfsParams.MountedPath,
	}).Debug("guest rootfs params")

	vmmType := u.State.Annotations[annotHypervisor]
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(vmmType), u.UruncCfg.Monitors)
	if err != nil {
		return err
	}

	defaultMemSizeMB := u.UruncCfg.Monitors[vmmType].DefaultMemoryMB
	memory := monitorMemoryBytes(defaultMemSizeMB, u.Spec.Linux.Resources)
	rfsBuilder := u.newRootfsBuilder(rootfsParams, unikernel, unikernelPath, initrdPath, memory)

	err = rfsBuilder.preSetup()
	if err != nil {
		return fmt.Errorf("pre setup step for rootfs failed: %w", err)
	}

	monRes, err := getMonitorResources(rfsBuilder, rootfsParams, vmm, u.UruncCfg.Monitors[vmmType].DataPath)
	if err != nil {
		return err
	}
	monRes.Rootfs = rootfsParams

	err = rfsBuilder.postSetup()
	if err != nil {
		return fmt.Errorf("post setup step for rootfs failed: %w", err)
	}

	u.State.Status = specs.StateCreating
	// FIXME: should we really create this base dir
	err = os.MkdirAll(u.BaseDir, 0o755)
	if err != nil {
		return err
	}

	err = saveMonitorResources(u.BaseDir, monRes)
	if err != nil {
		return fmt.Errorf("failed to store monitor resources: %w", err)
	}

	// In libcontainer mode the monitor process runs inside a dedicated rootfs and
	// cannot reach the state directory, so everything it needs is written into that
	// rootfs now.
	// TODO: Switch to fifo
	if u.UruncCfg.Runtime.Libcontainer {
		err = u.writeMonitorSpec(rootfsParams, monRes)
		if err != nil {
			return fmt.Errorf("failed to store the monitor spec: %w", err)
		}
	}

	return u.saveContainerState()
}

// Create sets the Unikernel status as created,
// and saves the given PID in the provided pid file path.
// If pidFilePath is empty, it falls back to the default init.pid path.
func (u *Unikontainer) Create(pid int, pidFilePath string) error {
	path := filepath.Join(u.State.Bundle, initPidFilename)
	if pidFilePath != "" {
		path = pidFilePath
	}
	err := writePidFile(path, pid)
	if err != nil {
		return err
	}
	u.State.Pid = pid
	u.State.Status = specs.StateCreated
	return u.saveContainerState()
}

// SetRunningState sets the Unikernel status as running,
func (u *Unikontainer) SetRunningState() error {
	u.State.Status = specs.StateRunning
	return u.saveContainerState()
}

// SetupNet creates the sandbox's network device (tap) in the current network
// namespace and returns its parameters; uid and gid own the tap device.
func SetupNet(networkType string, mounts []specs.Mount, uid, gid uint32) (types.NetDevParams, error) {
	uniklog.WithField("network type", networkType).Debug("Retrieved network type")
	netArgs := types.NetDevParams{}
	netManager, err := network.NewNetworkManager(networkType)
	if err != nil {
		return netArgs, fmt.Errorf("failed to create network manager for %s type: %v", networkType, err)
	}

	networkInfo, err := netManager.NetworkSetup(uid, gid)
	if err != nil {
		// TODO: Handle this case better. We do not need to show an error
		// since there was no network in the container. Therefore, we
		// need better error handling and specifically check if the container
		// di not have any network.
		uniklog.Errorf("Failed to setup network :%v. Possibly due to ctr", err)
	}
	// if network info is nil, we didn't find eth0, so we are running with ctr
	if networkInfo != nil {
		netArgs.TapDev = networkInfo.TapDevice
		netArgs.IP = networkInfo.EthDevice.IP
		netArgs.Mask = networkInfo.EthDevice.Mask
		netArgs.Gateway = networkInfo.EthDevice.DefaultGateway
		// The MAC address for the guest network device is the same as the
		// virtual ethernet interface inside the namespace
		netArgs.MAC = networkInfo.EthDevice.MAC
		netArgs.MTU = networkInfo.EthDevice.MTU
		netArgs.DNSServer = getDNSServer(mounts)
	}

	return netArgs, nil
}

// chooseRootfs determines the best rootfs configuration based on available options
// Priority order:
//  1. Initrd (if specified)
//  2. Explicit block device annotation (if mounted at /)
//  3. Container rootfs as block device (if MountRootfs=true and supported)
//  4. Container rootfs as shared-fs: virtiofs > 9pfs (if MountRootfs=true and supported)
//  5. No rootfs
func ChooseRootfs(bundle, specRoot string, annot map[string]string, cfg *UruncConfig) (types.RootfsParams, error) {
	bundleDir := filepath.Clean(bundle)
	rootfsDir := filepath.Clean(specRoot)
	rootfsDir, err := resolveAgainstBase(bundleDir, rootfsDir)
	if err != nil {
		uniklog.Errorf("could not resolve rootfs directory %s: %v", rootfsDir, err)
		return types.RootfsParams{}, err
	}

	if cfg == nil {
		return types.RootfsParams{}, fmt.Errorf("urunc config is required for guest rootfs selection")
	}

	unikernelType := annot[annotType]
	unikernel, err := unikernels.New(unikernelType)
	if err != nil {
		return types.RootfsParams{}, err
	}

	vmmType := annot[annotHypervisor]
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(vmmType), cfg.Monitors)
	if err != nil {
		return types.RootfsParams{}, err
	}

	virtiofsdConfig := cfg.ExtraBins["virtiofsd"]

	selector := &rootfsSelector{
		bundle:     bundleDir,
		cntrRootfs: rootfsDir,
		annot:      annot,
		unikernel:  unikernel,
		vmm:        vmm,
		vfsdPath:   virtiofsdConfig.Path,
	}

	// Priority 1: Initrd
	result, ok := selector.tryInitrd()
	if ok {
		return result, nil
	}

	// Priority 2: Explicit block annotation
	result, ok = selector.tryExplicitBlock()
	if ok {
		return result, nil
	}

	// Priority 3 & 4: Container rootfs (block or shared-fs)
	result, ok = selector.tryContainerRootfs()
	if ok {
		return switchMonRootfs(result, bundleDir)
	}

	if selector.shouldMountContainerRootfs() {
		return types.RootfsParams{}, fmt.Errorf("can not use the container rootfs as the sandbox's guest rootfs through block or shared-fs")
	}

	uniklog.Info("no rootfs configured for guest")
	result.MonRootfs = rootfsDir

	return result, nil
}

// getMonitorResources collects every mount and device required for the monitor's
// execution environment, plus the block parameters passed to the guest.
func getMonitorResources(rfs rootfsBuilder, rootfsParams types.RootfsParams, vmm types.VMM, monitorDataPath string) (monitorResources, error) {
	var res monitorResources
	var err error

	res.Mounts, err = mountsForMonitor(vmm.Path(), monitorDataPath)
	if err != nil {
		return res, err
	}
	rootfsMounts, err := rfs.getMounts()
	if err != nil {
		return res, fmt.Errorf("failed to get mounts for monitor rootfs: %w", err)
	}
	res.Mounts = append(res.Mounts, rootfsMounts...)

	res.Devices, err = getMonitorDevices(vmm.UsesKVM())
	if err != nil {
		return res, fmt.Errorf("failed to get host devices for monitor: %w", err)
	}
	res.BlockArgs, err = rfs.getBlockDevs()
	if err != nil {
		return res, fmt.Errorf("failed to get block devices to attach in sandbox: %w", err)
	}
	blockDevs, err := blockDevNodes(res.BlockArgs, rootfsParams)
	if err != nil {
		return res, fmt.Errorf("failed to get block devices which should be replicated inside monitor's execution environment: %w", err)
	}
	res.Devices = append(res.Devices, blockDevs...)

	res.Sharedfs, err = rfs.getSharedDirs()
	if err != nil {
		return res, fmt.Errorf("failed to get directories to share with sandbox: %w", err)
	}

	res.PreStartCmd = rfs.preStartCmd()

	return res, nil
}

// newRootfsBuilder constructs the rootfsBuilder matching the selected guest
// rootfs. It is shared by InitialSetup (which gathers the monitor resources) and
// Exec (which performs the per-rootfs actions).
func (u *Unikontainer) newRootfsBuilder(rootfsParams types.RootfsParams, unikernel types.Unikernel, unikernelPath string, initrdPath string, memory uint64) rootfsBuilder {
	switch rootfsParams.Type {
	case "block":
		return blockRootfs{
			mounts:        u.Spec.Mounts,
			monRootfs:     rootfsParams.MonRootfs,
			mountedPath:   rootfsParams.MountedPath,
			path:          rootfsParams.Path,
			kernelPath:    unikernelPath,
			initrdPath:    initrdPath,
			uruncJSONPath: uruncJSONFilename,
			guestType:     u.State.Annotations[annotType],
			guest:         unikernel,
		}
	case "initrd":
		return initrdRootfs{
			mounts:             u.Spec.Mounts,
			initrdHostFullPath: filepath.Join(rootfsParams.MonRootfs, rootfsParams.Path),
			monRootfs:          rootfsParams.MonRootfs,
			guestType:          u.State.Annotations[annotType],
		}
	case "virtiofs", "9pfs":
		return sharedfsRootfs{
			mounts:      u.Spec.Mounts,
			monRootfs:   rootfsParams.MonRootfs,
			mountedPath: rootfsParams.MountedPath,
			sfsType:     rootfsParams.Type,
			vfsdConfig:  u.UruncCfg.ExtraBins["virtiofsd"],
			sharedPath:  containerRootfsMountPath,
			memory:      memory,
		}
	default:
		return noRootfs{
			monRootfs:            rootfsParams.MonRootfs,
			annotBlockPath:       u.State.Annotations[annotBlock],
			annotBlockMountPoint: u.State.Annotations[annotBlockMntPoint],
		}
	}
}

// monitorMemoryBytes returns the guest memory size in bytes, honoring a memory
// limit from the OCI spec and falling back to the monitor's configured default.
func monitorMemoryBytes(defaultMem uint, resources *specs.LinuxResources) uint64 {
	mem := uint64(defaultMem * 1024 * 1024)
	if resources != nil && resources.Memory != nil {
		if resources.Memory.Limit != nil && *resources.Memory.Limit > 0 {
			mem = uint64(*resources.Memory.Limit) // nolint:gosec
		}
	}

	return mem
}

// buildMonitorSpec assembles the base MonitorSpec: everything the monitor needs
// that can be derived from the OCI spec, the container's annotations and the
// monitor resources gathered during InitialSetup.
func (u *Unikontainer) buildMonitorSpec(rootfsParams types.RootfsParams, monRes monitorResources) monitorSpec {
	var mSpec monitorSpec

	unikernelType := u.State.Annotations[annotType]
	vmmType := u.State.Annotations[annotHypervisor]
	unikernelVersion := u.State.Annotations[annotVersion]
	unikernelPath := u.State.Annotations[annotBinary]
	initrdPath := u.State.Annotations[annotInitrd]

	uniklog.WithFields(logrus.Fields{
		"vmm type":          vmmType,
		"unikernel type":    unikernelType,
		"unikernel version": unikernelVersion,
		"unikernel Path":    unikernelPath,
		"initrd Path":       initrdPath,
	}).Debug("Initialization values")

	defaultVCPUs := u.UruncCfg.Monitors[vmmType].DefaultVCPUs
	if defaultVCPUs < 1 {
		defaultVCPUs = 1
	}
	defaultMemSizeMB := u.UruncCfg.Monitors[vmmType].DefaultMemoryMB

	vmmArgs := types.ExecArgs{
		ContainerID:   u.State.ID,
		UnikernelPath: unikernelPath,
		InitrdPath:    initrdPath,
		Seccomp:       true, // Enable Seccomp by default
		MemSizeB:      monitorMemoryBytes(defaultMemSizeMB, u.Spec.Linux.Resources),
		VCPUs:         uint(defaultVCPUs),
		Environment:   os.Environ(),
	}

	// Check if container is set to unconfined -- disable seccomp
	if u.Spec.Linux.Seccomp == nil {
		uniklog.Warn("Seccomp is disabled")
		vmmArgs.Seccomp = false
	}

	guest := types.UnikernelParams{
		CmdLine: u.Spec.Process.Args,
		EnvVars: u.Spec.Process.Env,
		Monitor: vmmType,
		Version: unikernelVersion,
		ProcConf: types.ProcessConfig{
			UID:     u.Spec.Process.User.UID,
			GID:     u.Spec.Process.User.GID,
			WorkDir: u.Spec.Process.Cwd,
		},
		NetDevName: u.State.Annotations[annotNetDev],
		BlkDevName: u.State.Annotations[annotBlkDev],
		Rootfs:     rootfsParams,
		Block:      monRes.BlockArgs,
	}
	if len(guest.CmdLine) == 0 {
		guest.CmdLine = strings.Fields(u.State.Annotations[annotCmdLine])
	}

	if rootfsParams.Type == "virtiofs" || rootfsParams.Type == "9pfs" {
		// Update the paths of the files we need to pass in the monitor process.
		vmmArgs.UnikernelPath = adjustPathsForSharedfs(vmmArgs.UnikernelPath)
		vmmArgs.InitrdPath = adjustPathsForSharedfs(vmmArgs.InitrdPath)
	}
	vmmArgs.Sharedfs = monRes.Sharedfs

	mSpec.ContainerID = u.State.ID
	mSpec.UnikernelType = unikernelType
	mSpec.MonitorType = vmmType
	mSpec.MonitorCfg = u.UruncCfg.Monitors[vmmType]
	mSpec.ExecArgs = vmmArgs
	mSpec.GuestParams = guest
	mSpec.PreStartCmd = monRes.PreStartCmd

	return mSpec
}

// setupMonitorRootfs prepares the monitor rootfs: it makes sure the directory
// exists and is mounted with a propagation flag that allows a later pivot, then
// replicates the gathered mounts and devices inside it and gives the monitor a
// console.
func (u *Unikontainer) setupMonitorRootfs(monRootfs string, monRes monitorResources, withTUNTAP bool) error {
	err := os.MkdirAll(monRootfs, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create monitor rootfs directory %s: %w", monRootfs, err)
	}

	// Make sure that rootfs is mounted with the correct propagation
	// flags so we can later pivot if needed.
	err = prepareRoot(monRootfs, u.Spec.Linux.RootfsPropagation)
	if err != nil {
		return err
	}

	err = applyMounts(monRootfs, monRes.Mounts)
	if err != nil {
		return fmt.Errorf("failed to apply rootfs mounts: %w", err)
	}

	// setupDevices decides whether to create the TUN/TAP device based on the
	// container's network configuration.
	err = setupDevices(monRootfs, monRes.Devices, withTUNTAP)
	if err != nil {
		return fmt.Errorf("failed to create devices in monitor rootfs: %w", err)
	}

	err = setupConsole(monRootfs)
	if err != nil {
		return fmt.Errorf("failed to setup console: %w", err)
	}

	return nil
}

// nolint:gocyclo
func (u *Unikontainer) Exec(metrics m.Writer) error {
	metrics.Capture(m.TS15)

	// The chosen guest rootfs params, together with the monitor mounts, devices
	// and block args, were gathered and stored in monitor.json during
	// InitialSetup. Load them back here.
	monRes, err := loadMonitorResources(u.BaseDir)
	if err != nil {
		return fmt.Errorf("failed to load monitor resources: %w", err)
	}
	rootfsParams := monRes.Rootfs
	if rootfsParams.MonRootfs == "" {
		uniklog.Errorf("missing metadata for selected rootfs")
		return fmt.Errorf("missing metadata for rootfs preparation")
	}
	uniklog.WithFields(logrus.Fields{
		"rootfs_type": rootfsParams.Type,
		"rootfs_path": rootfsParams.Path,
		"mon_rootfs":  rootfsParams.MonRootfs,
	}).Debug("guest rootfs params")

	ms := u.buildMonitorSpec(rootfsParams, monRes)
	vmmArgs := ms.ExecArgs
	unikernelParams := ms.GuestParams

	// The spec carries the monitor and unikernel by type; rebuild the behaviour
	// objects it refers to, exactly as the monitor process does.
	unikernel, err := unikernels.New(ms.UnikernelType)
	if err != nil {
		return err
	}
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(ms.MonitorType), u.UruncCfg.Monitors)
	if err != nil {
		return err
	}

	// handle network
	netArgs, err := SetupNet(u.getNetworkType(), u.Spec.Mounts, u.Spec.Process.User.UID, u.Spec.Process.User.GID)
	if err != nil {
		uniklog.Errorf("failed to setup network: %v", err)
		return err
	}
	metrics.Capture(m.TS16)
	withTUNTAP := netArgs.IP != ""
	unikernelParams.Net = netArgs
	vmmArgs.Net = netArgs

	err = u.setupMonitorRootfs(rootfsParams.MonRootfs, monRes, withTUNTAP)
	if err != nil {
		return err
	}
	metrics.Capture(m.TS17)

	// vAccel setup
	vAccelType, vsockSocketPath, rpcAddress, err := resolveVAccelConfig(u.State.Annotations[annotHypervisor], u.Spec.Annotations)
	if err != nil {
		if !errors.Is(err, ErrVAccelDisabled) {
			uniklog.Warnf("vAccel misconfiguration: %v", err)
		}
	}

	if vAccelType == "vsock" && err == nil {
		// Remove any existing VACCEL_RPC_ADDRESS and set the new value
		for i, envVar := range unikernelParams.EnvVars {
			if strings.HasPrefix(envVar, "VACCEL_RPC_ADDRESS"+"=") {
				unikernelParams.EnvVars = remove(unikernelParams.EnvVars, i)
				break
			}
		}
		unikernelParams.EnvVars = append(unikernelParams.EnvVars, "VACCEL_RPC_ADDRESS="+rpcAddress)

		// Prepare the guest environment for vAccel vsock communication
		vaccelDevices, err := prepareVSockEnvironment(rootfsParams.MonRootfs, u.State.Annotations[annotHypervisor], vsockSocketPath)
		if err != nil {
			uniklog.Debugf("failed to prepare get required vsock devices: %v", err)
		}
		err = setupDevices(rootfsParams.MonRootfs, vaccelDevices, false)
		if err != nil {
			return fmt.Errorf("failed to create devices in monitor rootfs: %w", err)
		}

		vmmArgs.VAccelType = vAccelType
		vmmArgs.VSockDevPath = vsockSocketPath
		vmmArgs.VSockDevID = idToGuestCID(u.State.ID)
	}

	// unikernel
	// build the unikernel command
	vmmArgs.Command, err = buildUnikernelCommand(unikernel, unikernelParams)
	if err != nil {
		return err
	}

	// pivot
	_, err = findNS(u.Spec.Linux.Namespaces, specs.MountNamespace)
	// Only pivot if a mount namespace entry is actually present in the
	// spec, either to join (err is nil) or to create (err is
	// ErrNotExistingNS). If the entry is missing entirely, no new mount
	// namespace gets created and we have to chroot instead, otherwise
	// pivot_root would run against the caller's own root filesystem.
	withPivot := err == nil || errors.Is(err, ErrNotExistingNS)
	err = changeRoot(rootfsParams.MonRootfs, withPivot)
	if err != nil {
		return err
	}

	// uid/gid
	// Setup uid, gid and additional groups for the monitor process
	err = setupUser(u.Spec.Process.User)
	if err != nil {
		return err
	}

	// execute hooks
	// NOTE: StartContainer hooks are supposed to run right before the init of
	// the container. However, in the case of a Linux-based container, the init
	// of the container runs inside the sandbox. Therefore, we have to see how
	// we should treat this hook, because it might refer to operations like
	// ldconfig etc.
	err = u.ExecuteHooks("StartContainer")
	if err != nil {
		return err
	}

	err = spawnProcess(monRes.PreStartCmd)
	if err != nil {
		return err
	}

	// Build the VMM command once and verify it can be constructed successfully, so
	// we do not report the container as started if command building fails.
	execCmd, err := vmm.BuildExecCmd(vmmArgs, unikernel)
	if err != nil {
		uniklog.WithError(err).Error("failed to build VMM command")
		return err
	}

	// Notify urunc start that the monitor is ready to execute, only after the
	// command builds so a container is never reported started when it cannot be.
	err = u.SendMessage(StartSuccess)
	if err != nil {
		return err
	}

	return execMonitor(metrics, vmm, vmmArgs, execCmd)
}

// buildUnikernelCommand initializes the unikernel with the collected parameters
// and returns its command line.
func buildUnikernelCommand(unikernel types.Unikernel, params types.UnikernelParams) (string, error) {
	err := unikernel.Init(params)
	if errors.Is(err, unikernels.ErrUndefinedVersion) ||
		errors.Is(err, unikernels.ErrVersionParsing) {
		uniklog.WithError(err).Error("an error occurred while initializing the unikernel")
	} else if err != nil {
		return "", err
	}

	return unikernel.CommandString()
}

// execMonitor runs the monitor's pre-exec setup and finally execve's the monitor.
// It does not return on success:
//
// TODO: The container can still be reported as running if the PreExec step
// (e.g., BPF/seccomp filter setup) fails after the caller reported success. We
// should find a way to handle that case as well.
func execMonitor(metrics m.Writer, vmm types.VMM, execArgs types.ExecArgs, execCmd []string) error {
	uniklog.Debug("calling vmm execve")
	metrics.Capture(m.TS18)
	// Perform any monitor-specific pre-exec setup (e.g., seccomp filters for HVT).
	err := vmm.PreExec(execArgs)
	if err != nil {
		uniklog.WithError(err).Error("failed to perform pre-exec setup")
		return err
	}

	// Execute the VMM using the command we built earlier.
	uniklog.WithField("command", execCmd).Debug("Ready to execve VMM")
	return syscall.Exec(vmm.Path(), execCmd, execArgs.Environment) //nolint: gosec
}

func setupUser(user specs.User) error {
	runtime.LockOSThread()
	// Set the user for the current go routine to exec the Monitor
	AddGidsLen := len(user.AdditionalGids)
	if AddGidsLen > 0 {
		err := unix.Setgroups(convertUint32ToIntSlice(user.AdditionalGids, AddGidsLen))
		if err != nil {
			return fmt.Errorf("could not set Additional groups %v : %v", user.AdditionalGids, err)
		}
	}

	err := unix.Setgid(int(user.GID))
	if err != nil {
		return fmt.Errorf("could not set gid %d: %v", user.GID, err)
	}

	err = unix.Setuid(int(user.UID))
	if err != nil {
		return fmt.Errorf("could not set uid %d: %v", user.UID, err)
	}

	return nil
}

// Signal sends a specified signal to container's init.
func (u *Unikontainer) Signal(signal unix.Signal) error {
	vmmType := u.State.Annotations[annotHypervisor]
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(vmmType), u.UruncCfg.Monitors)
	if err != nil {
		return err
	}

	return vmm.Signal(u.State.Pid, signal)
}

// Kill stops the VMM process, first by asking the VMM struct to stop
// and consequently by killing the process described in u.State.Pid
func (u *Unikontainer) Kill() error {
	// Try to join the Network namespace of the monitor before killing it.
	// If we kill it there might be no process inside the namespace and hence
	// the namespace gets destroyed.
	err := u.joinSandboxNetNs()
	if err != nil {
		if errors.Is(err, ErrNotExistingNS) {
			// There is no network namespace to join.
			// Most probably the sandbox is dead and the namespace
			// has been destroyed.
			uniklog.Infof("could not find sandbox's network namespace: %v", err)
			return nil
		}
		return fmt.Errorf("failed to join sandbox netns: %v", err)
	}

	// get a new vmm
	vmmType := u.State.Annotations[annotHypervisor]
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(vmmType), u.UruncCfg.Monitors)
	if err != nil {
		return err
	}
	err = vmm.Stop(u.State.Pid)
	if err != nil {
		return err
	}

	err = network.CleanupAllUruncTaps()
	if err != nil {
		uniklog.Errorf("failed to cleanup tap devices: %v", err)
	}

	return nil
}

// Delete removes the containers base directory and its contents
func (u *Unikontainer) Delete() error {
	var dirs []string
	var prefPath string

	if u.isRunning() {
		return fmt.Errorf("cannot delete running container: %s", u.State.ID)
	}

	// In libcontainer mode, tear down the monitor's libcontainer state and cgroup.
	// Like runc.
	if u.UruncCfg.Runtime.Libcontainer {
		err := u.destroyLibcontainer()
		if err != nil {
			return err
		}
	}

	// Restore the block volume mounts that were unmounted during create,
	// so their sources become discoverable by future containers. Do it in
	// a best-effort way, since a failure to restore a mount should not
	// prevent the deletion of the container.
	monRes, err := loadMonitorResources(u.BaseDir)
	if err != nil {
		uniklog.Errorf("failed to load monitor resources: %v", err)
	}

	err = restoreBlockVolumes(monRes.BlockArgs)
	if err != nil {
		uniklog.Errorf("failed to restore block volume mounts: %v", err)
	}

	// get a monitor instance of the running monitor
	vmmType := u.State.Annotations[annotHypervisor]
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(vmmType), u.UruncCfg.Monitors)
	if err != nil {
		return err
	}

	// Make sure paths are clean
	bundleDir := filepath.Clean(u.State.Bundle)
	rootfsDir := filepath.Clean(u.Spec.Root.Path)
	if !filepath.IsAbs(rootfsDir) {
		rootfsDir = filepath.Join(bundleDir, rootfsDir)
	}
	monRootfs := filepath.Join(bundleDir, monitorRootfsDirName)

	// TODO: We might not need to remove any of the directories and let
	// the kernel cleanup the mounts and shim to remove directories.
	// However, just to be on the safe side, we remove all the newly
	// created directories from urunc. In order to check if we used the
	// rootfs under the bundle directory or we create anew one, we can check
	// if the monitorRootfsDirName directory exists under the bundle.
	_, err = os.Stat(monRootfs)
	if !os.IsNotExist(err) {
		// Since there was no block defined for the unikernel
		// and we created a new rootfs for the monitor, we need to
		// clean it up.
		dirs = append(dirs, monitorRootfsDirName)
		prefPath = bundleDir
	} else {
		// Otherwise remove the enw directories we created and the monitor spec
		// file inside the container's rootfs.
		// We do not need to unmount anything here, since we rely on Linux
		// to do the cleanup for us. This will happen automatically,
		// when the mount namespace gets destroyed
		err = RemoveMonitorSpec(rootfsDir)
		// Ignore the case where the file does not exist.
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to remove the monitor spec: %w", err)
		}

		dirs = []string{
			"/lib",
			"/lib64",
			"/usr",
			"/proc",
			"/dev",
			"/tmp",
		}
		dirs = append(dirs, vmm.Path())
		prefPath = rootfsDir
	}

	err = rmMultipleDirs(prefPath, dirs)
	if err != nil {
		return err
	}

	return os.RemoveAll(u.BaseDir)
}

// joinSandboxNetns joins the network namespace of the sandbox
// This function should be called only from a locked thread
// (i.e. runtime. LockOSThread())
func (u Unikontainer) joinSandboxNetNs() error {
	netNsPath, err := findNS(u.Spec.Linux.Namespaces, specs.NetworkNamespace)
	if err != nil && !errors.Is(err, ErrNotExistingNS) {
		return err
	}
	// In case no path was specified for the network namespace it means,
	// that we had to create a new one and therefore we can join it by
	// using the pid of the monitor process.
	if netNsPath == "" {
		netNsPath = fmt.Sprintf("/proc/%d/ns/net", u.State.Pid)
		err := checkValidNsPath(netNsPath)
		if err != nil {
			return err
		}
	}
	uniklog.WithFields(logrus.Fields{
		"path": netNsPath,
	}).Debug("Joining network namespace")
	fd, err := unix.Open(netNsPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("error opening namespace path: %w", err)
	}
	err = unix.Setns(int(fd), unix.CLONE_NEWNET)
	if err != nil {
		return fmt.Errorf("error joining namespace: %w", err)
	}
	uniklog.Debug("Joined network namespace")
	return nil
}

// Saves current Unikernel state as baseDir/state.json for later use
func (u *Unikontainer) saveContainerState() error {
	// Propagate all annotations from spec to state to solve nerdctl hooks errors.
	// For more info: https://github.com/containerd/nerdctl/issues/133
	for key, value := range u.Spec.Annotations {
		if _, ok := u.State.Annotations[key]; !ok {
			u.State.Annotations[key] = value
		}
	}

	data, err := json.Marshal(u.State)
	if err != nil {
		return err
	}

	stateName := filepath.Join(u.BaseDir, stateFilename)
	return os.WriteFile(stateName, data, 0o644) //nolint: gosec
}

// getHooksByName returns the hooks for a given lifecycle stage
func (u *Unikontainer) getHooksByName(name string) []specs.Hook {
	switch name {
	case "CreateRuntime":
		return u.Spec.Hooks.CreateRuntime
	case "CreateContainer":
		return u.Spec.Hooks.CreateContainer
	case "StartContainer":
		return u.Spec.Hooks.StartContainer
	case "Poststart":
		return u.Spec.Hooks.Poststart
	case "Poststop":
		return u.Spec.Hooks.Poststop
	default:
		uniklog.Warnf("Unsupported hook %s", name)
		return nil
	}
}

func (u *Unikontainer) ExecuteHooks(name string) error {
	if u.Spec.Hooks == nil {
		return nil
	}

	hooks := u.getHooksByName(name)
	uniklog.Debugf("Executing %d %s hooks", len(hooks), name)

	s, err := json.Marshal(u.State)
	if err != nil {
		return err
	}

	// NOTE: This wrapper function provides an easy way to toggle between
	// the sequential and concurrent hook execution.
	// By default the hooks are executed concurrently.
	// To execute hooks sequentially, change the following line to:
	// if false
	if true {
		return u.executeHooksConcurrently(name, hooks, s)
	}
	return u.executeHooksSequentially(name, hooks, s)
}

// executeHooksConcurrently executes concurrently any hooks found in spec based on name:
// NOTE: It is possible that the concurrent execution of the hooks may cause
// some unknown problems down the line. Be sure to prioritize checking
// with sequential hook execution when debugging.
func (u *Unikontainer) executeHooksConcurrently(name string, hooks []specs.Hook, s []byte) error {
	var (
		wg       sync.WaitGroup
		errChan  = make(chan error, len(hooks))
		firstErr error
	)
	for i := range hooks {
		uniklog.WithFields(logrus.Fields{
			"id":   u.State.ID,
			"name": name,
			"path": hooks[i].Path,
			"args": hooks[i].Args,
		}).Debug("Executing hook")

		wg.Add(1)
		go func(h specs.Hook) {
			defer wg.Done()
			err := executeHook(h, s)
			if err != nil {
				uniklog.WithFields(logrus.Fields{
					"id":    u.State.ID,
					"name":  name,
					"path":  h.Path,
					"args":  h.Args,
					"error": err,
				}).Error("Executing hook failed")
				errChan <- err
			}
		}(hooks[i])
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	for err := range errChan {
		uniklog.WithField("error", err.Error()).Error("failed to execute hook")
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// executeHooksSequentially executes sequentially any hooks found in spec based on name:
// NOTE: This function is left on purpose to aid future debugging efforts
// in case concurrent hook execution causes unexpected errors.
func (u *Unikontainer) executeHooksSequentially(name string, hooks []specs.Hook, s []byte) error {
	for i := range hooks {
		uniklog.WithFields(logrus.Fields{
			"id":   u.State.ID,
			"name": name,
			"path": hooks[i].Path,
			"args": hooks[i].Args,
		}).Debug("Executing hook")

		err := executeHook(hooks[i], s)
		if err != nil {
			uniklog.WithFields(logrus.Fields{
				"id":    u.State.ID,
				"name":  name,
				"path":  hooks[i].Path,
				"args":  hooks[i].Args,
				"error": err,
			}).Error("Executing hook failed")
			return fmt.Errorf("failed to execute %s hook: %w", name, err)
		}

	}
	return nil
}

// loadUnikontainerState returns a specs.State object containing the info
// found in stateFilePath
func loadUnikontainerState(stateFilePath string) (*specs.State, error) {
	var state specs.State
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, &state)
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// nolint:gocyclo
// FormatNsenterInfo encodes namespace info in netlink binary format
// as a io.Reader, in order to send the info to nsenter.
// The implementation is inspired from:
// https://github.com/opencontainers/runc/blob/c8737446d2f99c1b7f2fcf374a7ee5b4519b2051/libcontainer/container_linux.go#L1047
func (u *Unikontainer) FormatNsenterInfo() (rdr io.Reader, retErr error) {
	r := nl.NewNetlinkRequest(int(initMsg), 0)

	// Our custom messages cannot bubble up an error using returns, instead
	// they will panic with the specific error type, netlinkError. In that
	// case, recover from the panic and return that as an error.
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(netlinkError); ok {
				retErr = e.error
			} else {
				panic(r)
			}
		}
	}()

	const numNS = 8
	var writePaths bool
	var writeFlags bool
	var cloneFlags uint32
	var nsPaths [numNS]string // We have 8 namespaces right now
	// We need to set the namespace paths in a specific order.
	// The order should be: user, ipc, uts, net, pid, mount, cgroup, time
	// Therefore, the first element of the above array holds the path of user
	// namespace, while the last element, the time namespace path
	// Order does not matter in clone flags
	for _, ns := range u.Spec.Linux.Namespaces {
		// If the path is empty, then we have to create it.
		// Otherwise, we store the path to the respective element
		// of the array.
		switch ns.Type {
		// Comment out User namespace for the time being and just ignore them
		// They require better handling for cleaning up and we will address
		// it in another iteration.
		// TODO User namespace
		// case specs.UserNamespace:
		// 	if ns.Path == "" {
		// 		cloneFlags |= unix.CLONE_NEWUSER
		// 	} else {
		// 		err := checkValidNsPath(ns.Path)
		// 		if err == nil {
		// 			nsPaths[0] = "user:" + ns.Path
		// 		} else {
		// 			return nil, err
		// 		}
		// 	}
		case specs.IPCNamespace:
			if ns.Path == "" {
				cloneFlags |= unix.CLONE_NEWIPC
			} else {
				err := checkValidNsPath(ns.Path)
				if err == nil {
					nsPaths[1] = "ipc:" + ns.Path
				} else {
					return nil, err
				}
			}
		case specs.UTSNamespace:
			if ns.Path == "" {
				cloneFlags |= unix.CLONE_NEWUTS
			} else {
				err := checkValidNsPath(ns.Path)
				if err == nil {
					nsPaths[2] = "uts:" + ns.Path
				} else {
					return nil, err
				}
			}
		case specs.NetworkNamespace:
			if ns.Path == "" {
				cloneFlags |= unix.CLONE_NEWNET
			} else {
				err := checkValidNsPath(ns.Path)
				if err == nil {
					nsPaths[3] = "net:" + ns.Path
				} else {
					return nil, err
				}
			}
		case specs.PIDNamespace:
			if ns.Path == "" {
				cloneFlags |= unix.CLONE_NEWPID
			} else {
				err := checkValidNsPath(ns.Path)
				if err == nil {
					nsPaths[4] = "pid:" + ns.Path
				} else {
					return nil, err
				}
			}
		case specs.MountNamespace:
			if ns.Path == "" {
				cloneFlags |= unix.CLONE_NEWNS
			} else {
				err := checkValidNsPath(ns.Path)
				if err == nil {
					nsPaths[5] = "mnt:" + ns.Path
				} else {
					return nil, err
				}
			}
		case specs.CgroupNamespace:
			if ns.Path == "" {
				cloneFlags |= unix.CLONE_NEWCGROUP
			} else {
				err := checkValidNsPath(ns.Path)
				if err == nil {
					nsPaths[6] = "cgroup:" + ns.Path
				} else {
					return nil, err
				}
			}
		case specs.TimeNamespace:
			if ns.Path == "" {
				cloneFlags |= unix.CLONE_NEWTIME
			} else {
				err := checkValidNsPath(ns.Path)
				if err == nil {
					nsPaths[7] = "time:" + ns.Path
				} else {
					return nil, err
				}
			}
		default:
			uniklog.Warnf("Unsupported namespace: %s. It will get ignored", ns.Type)
		}
		if ns.Path == "" {
			writeFlags = true
		} else {
			writePaths = true
		}
	}

	if writeFlags {
		r.AddData(&int32msg{
			Type:  cloneFlagsAttr,
			Value: uint32(cloneFlags),
		})
	}

	var nsBuf bytes.Buffer
	if writePaths {
		for i := 0; i < numNS; i++ {
			if nsPaths[i] != "" {
				if nsBuf.Len() > 0 {
					nsBuf.WriteString(",")
				}
				nsBuf.WriteString(nsPaths[i])
			}
		}

		r.AddData(&bytemsg{
			Type:  nsPathsAttr,
			Value: nsBuf.Bytes(),
		})

	}

	// Setup uid/gid mappings only in the case we need to create a new
	// user namespace. As far as I understand (and I might be very wrong),
	// we can set up the uid/gid mappings only once in a user namespace.
	// Therefore, if we enter a user namespace and try to set the uid/gid
	// mappings, we will get EPERM. Therefore, it is important to note that
	// according to runc, when the config instructs us to use an existing
	// user namespace, the uid/gid mappings should be empty and hence
	// inherit the ones that are already set. Check:
	// https://github.com/opencontainers/runc/blob/e0e22d33eabc4dc280b7ca0810ed23049afdd370/libcontainer/specconv/spec_linux.go#L1036

	// TODO: Add it when we add user namespaces
	// if nsPaths[0] == "" {
	// 	// write uid mappings
	// 	if len(u.Spec.Linux.UIDMappings) > 0 {
	// 		// TODO: Rootless
	// 		b, err := encodeIDMapping(u.Spec.Linux.UIDMappings)
	// 		if err != nil {
	// 			return nil, err
	// 		}
	// 		r.AddData(&bytemsg{
	// 			Type:  uidmapAttr,
	// 			Value: b,
	// 		})
	// 	}
	// 	// write gid mappings
	// 	if len(u.Spec.Linux.GIDMappings) > 0 {
	// 		b, err := encodeIDMapping(u.Spec.Linux.GIDMappings)
	// 		if err != nil {
	// 			return nil, err
	// 		}
	// 		r.AddData(&bytemsg{
	// 			Type:  gidmapAttr,
	// 			Value: b,
	// 		})
	// 		// TODO: Rootless
	// 	}
	// }

	return bytes.NewReader(r.Serialize()), nil
}

// CreateListener creates a new listener over a Unix socket.
// If the caller is reexec then the new listener will refer to the
// ReexecSock, the socket that holds messages from urunc instances to the reexec process
// If it is not the reexec process then the listener will refer to the
// uruncSock, the socket that holds messages from reexec to urunc instances
func (u *Unikontainer) CreateListener(isReexec bool) error {
	// In libcontainer mode the start side reads the monitor's outcome from a FIFO
	// in the state dir, whose write end the monitor inherited from create.
	if !isReexec && u.UruncCfg.Runtime.Libcontainer {
		return u.openReadReadyPipe()
	}

	sockAddr := getUruncSockAddr(u.BaseDir)
	if isReexec {
		sockAddr = getReexecSockAddr(u.BaseDir)
	}

	listener, err := createListener(sockAddr, true)
	if err != nil {
		uniklog.WithError(err).Errorf("failed to create listener at %s", sockAddr)
		return fmt.Errorf("failed to create listener at %s: %w", sockAddr, err)
	}

	u.Listener = listener

	return nil
}

// DestroyListener destroys an existing listener over a socket
func (u *Unikontainer) DestroyListener(isReexec bool) error {
	if !isReexec && u.UruncCfg.Runtime.Libcontainer {
		return u.closeReadReadyPipe()
	}

	sockAddr := getUruncSockAddr(u.BaseDir)
	if isReexec {
		sockAddr = getReexecSockAddr(u.BaseDir)
	}
	listener := u.Listener

	// NOTE: In Go, Close() will also unlink the unix socket.
	err := listener.Close()
	if err != nil {
		uniklog.WithError(err).Errorf("failed to close listener at %s", sockAddr)
		err = fmt.Errorf("failed to close listener at %s: %w", sockAddr, err)
	}

	return err
}

// CreateConn opens a new connection to a unix socket.
// If the caller is reexec then the new connection will refer to the
// uruncSock, the socket that holds messages from reexec to urunc instances
// If it is not the reexec process then the connection will refer to the
// ReexecSock, the socket that holds messages from urunc instances to the reexec process
func (u *Unikontainer) CreateConn(isReexec bool) error {
	sockAddr := getReexecSockAddr(u.BaseDir)
	if isReexec {
		sockAddr = getUruncSockAddr(u.BaseDir)
	}

	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sockAddr, Net: "unix"})
	if err != nil {
		uniklog.WithError(err).Errorf("failed to create connection to unix socket %s", sockAddr)
		return fmt.Errorf("failed to create connection to unix socket %s: %w", sockAddr, err)
	}

	u.Conn = conn

	return nil
}

// DestroyListenerReexec destroys an existing listener over a socket
func (u *Unikontainer) DestroyConn(isReexec bool) error {
	sockAddr := getReexecSockAddr(u.BaseDir)
	if isReexec {
		sockAddr = getUruncSockAddr(u.BaseDir)
	}
	conn := u.Conn

	err := conn.Close()
	if err != nil {
		uniklog.WithError(err).Errorf("failed to close connection to unix socket %s", sockAddr)
		return fmt.Errorf("failed to close connection to unix socket %s: %w", sockAddr, err)
	}

	return nil
}

// AwaitMessage waits for a specific message in the listener of unikontainer instance
func (u *Unikontainer) AwaitMsg(msg IPCMessage) error {
	// In libcontainer mode the monitor reports over the ready FIFO.
	if u.UruncCfg.Runtime.Libcontainer {
		return u.awaitReadyPipe()
	}
	return AwaitMessage(u.Listener, msg)
}

// SendMessage sends message over the active connection
func (u *Unikontainer) SendMessage(message IPCMessage) error {
	conn := u.Conn
	_, err := conn.Write([]byte(message))
	if err != nil {
		uniklog.WithError(err).Errorf("failed to send message %s", message)
		return fmt.Errorf("failed to send message %s over active connection: %w", message, err)
	}

	return nil
}

// isRunning returns true if the PID is alive or hedge.ListVMs returns our containerID
func (u *Unikontainer) isRunning() bool {
	vmmType := hypervisors.VmmType(u.State.Annotations[annotHypervisor])
	if vmmType != hypervisors.HedgeVmm {
		return syscall.Kill(u.State.Pid, syscall.Signal(0)) == nil
	}
	hedge := hypervisors.Hedge{}
	state := hedge.VMState(u.State.ID)
	return state == "running"
}

// getNetworkType checks if current container is a knative user-container
func (u Unikontainer) getNetworkType() string {
	if u.Spec.Annotations["io.kubernetes.cri.container-name"] == "user-container" {
		return "static"
	}
	return "dynamic"
}
