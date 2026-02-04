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
	"github.com/urunc-dev/urunc/pkg/unikontainers/initrd"
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
}

// New parses the bundle and creates a new Unikontainer object
func New(bundlePath string, containerID string, rootDir string, cfg *UruncConfig) (*Unikontainer, error) {
	spec, err := loadSpec(bundlePath)
	if err != nil {
		return nil, err
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
	u.BaseDir = containerDir
	u.RootDir = rootDir
	u.Spec = spec
	u.UruncCfg = UruncConfigFromMap(state.Annotations)
	return u, nil
}

// InitialSetup sets the Unikernel status as creating,
// creates the Unikernel base directory and
// saves the state.json file with the current Unikernel state
func (u *Unikontainer) InitialSetup() error {
	u.State.Status = specs.StateCreating
	// FIXME: should we really create this base dir
	err := os.MkdirAll(u.BaseDir, 0o755)
	if err != nil {
		return err
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

func (u *Unikontainer) SetupNet() (types.NetDevParams, error) {
	networkType := u.getNetworkType()
	uniklog.WithField("network type", networkType).Debug("Retrieved network type")
	netArgs := types.NetDevParams{}
	netManager, err := network.NewNetworkManager(networkType)
	if err != nil {
		return netArgs, fmt.Errorf("failed to create network manager for %s type: %v", networkType, err)
	}

	networkInfo, err := netManager.NetworkSetup(u.Spec.Process.User.UID, u.Spec.Process.User.GID)
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
	}

	return netArgs, nil
}

// nolint:gocyclo
func (u *Unikontainer) Exec(metrics m.Writer) error {
	metrics.Capture(m.TS15)

	// container Paths
	// Make sure paths are clean
	bundleDir := filepath.Clean(u.State.Bundle)
	rootfsDir := filepath.Clean(u.Spec.Root.Path)
	rootfsDir, err := resolveAgainstBase(bundleDir, rootfsDir)
	if err != nil {
		uniklog.Errorf("could not resolve rootfs directory %s: %v", rootfsDir, err)
		return err
	}

	// unikernel
	unikernelType := u.State.Annotations[annotType]
	unikernel, err := unikernels.New(unikernelType)
	if err != nil {
		return err
	}

	// Vmm
	vmmType := u.State.Annotations[annotHypervisor]
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(vmmType), u.UruncCfg.Monitors)
	if err != nil {
		return err
	}

	// unikernelParams
	unikernelVersion := u.State.Annotations[annotVersion]

	// ExecArgs
	unikernelPath := u.State.Annotations[annotBinary]
	initrdPath := u.State.Annotations[annotInitrd]

	// debug
	uniklog.WithFields(logrus.Fields{
		"bundle directory":  bundleDir,
		"rootfs directory":  rootfsDir,
		"vmm type":          vmmType,
		"unikernel type":    unikernelType,
		"unikernel version": unikernelVersion,
		"unikernel Path":    unikernelPath,
		"initrd Path":       initrdPath,
	}).Debug("Initialization values")

	// ExecArgs
	defaultVCPUs := u.UruncCfg.Monitors[vmmType].DefaultVCPUs
	if defaultVCPUs < 1 {
		defaultVCPUs = 1
	}
	defaultMemSizeMB := u.UruncCfg.Monitors[vmmType].DefaultMemoryMB

	// ExecArgs
	vmmArgs := types.ExecArgs{
		ContainerID:   u.State.ID,
		UnikernelPath: unikernelPath,
		InitrdPath:    initrdPath,
		Seccomp:       true, // Enable Seccomp by default
		MemSizeB:      uint64(defaultMemSizeMB * 1024 * 1024),
		VCPUs:         uint(defaultVCPUs),
		Environment:   os.Environ(),
	}

	// ExecArgs
	// If memory limit is set in spec, use it instead of the config default value
	if u.Spec.Linux.Resources.Memory != nil {
		if u.Spec.Linux.Resources.Memory.Limit != nil {
			if *u.Spec.Linux.Resources.Memory.Limit > 0 {
				vmmArgs.MemSizeB = uint64(*u.Spec.Linux.Resources.Memory.Limit) // nolint:gosec
			}
		}
	}

	// ExecArgs
	// Check if container is set to unconfined -- disable seccomp
	if u.Spec.Linux.Seccomp == nil {
		uniklog.Warn("Seccomp is disabled")
		vmmArgs.Seccomp = false
	}

	procAttrs := types.ProcessConfig{
		UID:     u.Spec.Process.User.UID,
		GID:     u.Spec.Process.User.GID,
		WorkDir: u.Spec.Process.Cwd,
	}
	// UnikernelParams
	// populate unikernel params
	unikernelParams := types.UnikernelParams{
		CmdLine:  u.Spec.Process.Args,
		EnvVars:  u.Spec.Process.Env,
		Monitor:  vmmType,
		Version:  unikernelVersion,
		ProcConf: procAttrs,
	}
	if len(unikernelParams.CmdLine) == 0 {
		unikernelParams.CmdLine = strings.Fields(u.State.Annotations[annotCmdLine])
	}

	// handle network
	netArgs, err := u.SetupNet()
	if err != nil {
		uniklog.Errorf("failed to setup network: %v", err)
		return err
	}
	metrics.Capture(m.TS16)
	withTUNTAP := netArgs.IP != ""

	// UnikernelParams
	unikernelParams.Net = netArgs

	// ExecArgs
	vmmArgs.Net = netArgs

	// virtiofsd config
	virtiofsdConfig := u.UruncCfg.ExtraBins["virtiofsd"]

	// guest rootfs
	// block
	// handle guest's rootfs.
	// There are three options:
	// 1. No rootfs for guest
	// 2. Use the devmapper snapshot as a block device for the guest's rootfs
	// 3. Use 9pfs to share the container's rootfs as the guest's rootfs
	// By default, urunc will not set any rootfs for the guest. However,
	// if the respective annotation is set then, depending on the guest
	// (supports block or 9pfs), it will use the supported option. In case
	// both ae supported, then the block option will be used by default.
	rootfsParams, err := chooseRootfs(bundleDir, rootfsDir, u.State.Annotations, unikernel, vmm, virtiofsdConfig.Path)
	if err != nil {
		uniklog.Errorf("could not choose guest rootfs: %v", err)
		return err
	}

	// Prepare Monitor rootfs
	// Make sure that rootfs is mounted with the correct propagation
	// flags so we can later pivot if needed.
	err = prepareRoot(rootfsParams.MonRootfs, u.Spec.Linux.RootfsPropagation)
	if err != nil {
		return err
	}

	// Setup the rootfs for the monitor execution, creating necessary
	// devices and the monitor's binary.
	err = prepareMonRootfs(rootfsParams.MonRootfs, vmm.Path(), u.UruncCfg.Monitors[vmmType].DataPath, vmm.UsesKVM(), withTUNTAP)
	if err != nil {
		return err
	}
	// TODO: Add support for using both an existing
	// block based snapshot of the container's rootfs
	// and an auxiliary block image placed in the container's image
	// Currently if a block Image is present in the container's image, then
	// we will just use this image.
	blockArgs := []types.BlockDevParams{}
	sharedfsArgs := types.SharedfsParams{}
	tmpfsSize := "65536k"
	switch rootfsParams.Type {
	case "block":
		blockArgs, err = handleBlockBasedRootfs(rootfsParams, unikernel, unikernelType, unikernelPath, uruncJSONFilename, initrdPath, u.Spec.Mounts)
		if err != nil {
			uniklog.Errorf("could not setup block based rootfs: %v", err)
			return err
		}
	case "initrd":
		initrdHostFullPath := filepath.Join(rootfsParams.MonRootfs, rootfsParams.Path)
		err = initrd.CopyFileMountsToInitrd(initrdHostFullPath, u.Spec.Mounts)
		if err != nil {
			uniklog.Errorf("could not update guest's initrd: %v", err)
			return err
		}
	case "virtiofs":
		tmpfsSize = chooseTmpfsSize(vmmArgs.MemSizeB)
		fallthrough
	case "9pfs":
		err = setupSharedfsBasedRootfs(rootfsParams, virtiofsdConfig.Path, u.Spec.Mounts)
		if err != nil {
			return err
		}
		// Update the paths of the files we need to pass in the monitor process.
		vmmArgs.UnikernelPath = adjustPathsForSharedfs(vmmArgs.UnikernelPath)
		vmmArgs.InitrdPath = adjustPathsForSharedfs(vmmArgs.InitrdPath)
		sharedfsArgs.Path = containerRootfsMountPath
		sharedfsArgs.Type = rootfsParams.Type
	default:
		uniklog.Debug("No rootfs for guest")
	}
	unikernelParams.Rootfs = rootfsParams

	err = createTmpfs(rootfsParams.MonRootfs, "/tmp",
		unix.MS_NOSUID|unix.MS_NOEXEC|unix.MS_STRICTATIME,
		"1777", tmpfsSize)
	if err != nil {
		return err
	}
	metrics.Capture(m.TS17)

	blockFromAnnot, err := handleExplicitBlockImage(u.State.Annotations[annotBlock],
		u.State.Annotations[annotBlockMntPoint])
	if err != nil {
		return err
	}
	if blockFromAnnot.Source != "" && blockFromAnnot.MountPoint != "/" {
		// TODO: Add proper support for multiple block Images from the container's
		// image. This requires adding more annotations too.
		blockFromAnnot.ID = "annot_vol"
		blockArgs = append(blockArgs, blockFromAnnot)
	}

	// unikernelParams
	unikernelParams.Block = blockArgs

	// ExecArgs
	vmmArgs.Sharedfs = sharedfsArgs

	// vAccel setup
	vAccelType, vsockSocketPath, rpcAddress, err := resolveVAccelConfig(u.State.Annotations[annotHypervisor], u.Spec.Annotations)
	if err != nil {
		uniklog.Debugf("vAccel config: %v", err)
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
		err = prepareVSockEnvironment(rootfsParams.MonRootfs, u.State.Annotations[annotHypervisor], vsockSocketPath)
		if err != nil {
			uniklog.Debugf("failed to prepare all required vsock mounts: %v", err)
		}

		vmmArgs.VAccelType = vAccelType
		vmmArgs.VSockDevPath = vsockSocketPath
		vmmArgs.VSockDevID = idToGuestCID(u.State.ID)
	}

	// unikernel
	err = unikernel.Init(unikernelParams)
	if err == unikernels.ErrUndefinedVersion || err == unikernels.ErrVersionParsing {
		uniklog.WithError(err).Error("an error occurred while initializing the unikernel")
	} else if err != nil {
		return err
	}

	// unikernel
	// build the unikernel command
	unikernelCmd, err := unikernel.CommandString()
	if err != nil {
		return err
	}

	// ExecArgs
	vmmArgs.Command = unikernelCmd

	// pivot
	_, err = findNS(u.Spec.Linux.Namespaces, specs.MountNamespace)
	// We just want to check if a mount namespace was define din the list
	// Therefore, if there was no error and the mount namespace was found
	// we can pivot.
	withPivot := err != nil
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

	// virtiofs
	if rootfsParams.Type == "virtiofs" {
		// Start the virtiofsd process
		err = spawnVirtiofsd(virtiofsdConfig, containerRootfsMountPath)
		if err != nil {
			return err
		}
	}

	uniklog.Debug("calling vmm execve")
	metrics.Capture(m.TS18)

	// Build the VMM command once and verify it can be constructed successfully.
	// This ensures we don't report the container as started if command building fails.
	execCmd, err := vmm.BuildExecCmd(vmmArgs, unikernel)
	if err != nil {
		uniklog.WithError(err).Error("failed to build VMM command")
		return err
	}

	// Notify urunc start that the monitor is ready to execute.
	// We send this after BuildExecCmd succeeds to avoid reporting a container
	// as started when the VMM command cannot be built.
	// TODO: The container can still be reported as running if the PreExec step
	// (e.g., BPF/seccomp filter setup) fails after this point. We should find
	// a way to handle that case as well.
	err = u.SendMessage(StartSuccess)
	if err != nil {
		return err
	}

	// Perform any monitor-specific pre-exec setup (e.g., seccomp filters for HVT).
	err = vmm.PreExec(vmmArgs)
	if err != nil {
		uniklog.WithError(err).Error("failed to perform pre-exec setup")
		return err
	}

	// Execute the VMM using the command we built earlier.
	uniklog.WithField("command", execCmd).Debug("Ready to execve VMM")
	return syscall.Exec(vmm.Path(), execCmd, vmmArgs.Environment) //nolint: gosec
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

	// TODO: tap0_urunc should not be hardcoded
	err = network.Cleanup("tap0_urunc")
	if err != nil {
		uniklog.Errorf("failed to delete tap0_urunc: %v", err)
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
		// Otherwise remove the enw directories we created inside the
		// container's rootfs.
		// We do not need to unmount anything here, since we rely on Linux
		// to do the cleanup for us. This will happen automatically,
		// when the mount namespace gets destroyed
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

	var nsStringBuilder strings.Builder
	if writePaths {
		for i := 0; i < numNS; i++ {
			if nsPaths[i] != "" {
				if nsStringBuilder.Len() > 0 {
					nsStringBuilder.WriteString(",")
				}
				nsStringBuilder.WriteString(nsPaths[i])
			}
		}

		r.AddData(&bytemsg{
			Type:  nsPathsAttr,
			Value: []byte(nsStringBuilder.String()),
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
	vmmType := hypervisors.VmmType(u.State.Annotations[annotType])
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
