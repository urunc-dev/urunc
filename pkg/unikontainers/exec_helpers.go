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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// buildVMMArgs constructs the ExecArgs for the VMM from the unikontainer spec and config.
func (u *Unikontainer) buildVMMArgs(vmmType, unikernelPath, initrdPath string) (types.ExecArgs, error) {
	defaultVCPUs := u.UruncCfg.Monitors[vmmType].DefaultVCPUs
	if defaultVCPUs < 1 {
		defaultVCPUs = 1
	}
	defaultMemSizeMB := u.UruncCfg.Monitors[vmmType].DefaultMemoryMB

	args := types.ExecArgs{
		ContainerID:   u.State.ID,
		UnikernelPath: unikernelPath,
		InitrdPath:    initrdPath,
		Seccomp:       true,
		MemSizeB:      uint64(defaultMemSizeMB * 1024 * 1024),
		VCPUs:         uint(defaultVCPUs),
		Environment:   os.Environ(),
	}

	if u.Spec.Linux.Resources != nil && u.Spec.Linux.Resources.Memory != nil {
		if u.Spec.Linux.Resources.Memory.Limit != nil {
			if *u.Spec.Linux.Resources.Memory.Limit > 0 {
				args.MemSizeB = uint64(*u.Spec.Linux.Resources.Memory.Limit) //nolint:gosec
			}
		}
	}

	if u.Spec.Linux.Seccomp == nil {
		uniklog.Warn("Seccomp is disabled")
		args.Seccomp = false
	}

	return args, nil
}

// buildUnikernelParams constructs the UnikernelParams from the unikontainer spec.
func (u *Unikontainer) buildUnikernelParams(vmmType, unikernelVersion string) types.UnikernelParams {
	procAttrs := types.ProcessConfig{
		UID:     u.Spec.Process.User.UID,
		GID:     u.Spec.Process.User.GID,
		WorkDir: u.Spec.Process.Cwd,
	}

	params := types.UnikernelParams{
		CmdLine:  u.Spec.Process.Args,
		EnvVars:  u.Spec.Process.Env,
		Monitor:  vmmType,
		Version:  unikernelVersion,
		ProcConf: procAttrs,
	}

	if len(params.CmdLine) == 0 {
		params.CmdLine = strings.Fields(u.State.Annotations[annotCmdLine])
	}

	return params
}

// setupRootfs selects and prepares the guest rootfs, returning the builder and params.
func (u *Unikontainer) setupRootfs(
	vmmType, unikernelType, unikernelPath, initrdPath string,
	unikernel types.Unikernel,
	vmm types.VMM,
	vmmArgs *types.ExecArgs,
	withTUNTAP bool,
) (rootfsBuilder, types.RootfsParams, error) {
	rootfsParams, err := u.chooseRootfs()
	if err != nil {
		return nil, types.RootfsParams{}, fmt.Errorf("setupRootfs: choose rootfs: %w", err)
	}

	virtiofsdConfig := u.UruncCfg.ExtraBins["virtiofsd"]

	var rfsBuilder rootfsBuilder
	switch rootfsParams.Type {
	case "block":
		rfsBuilder = blockRootfs{
			mounts:        u.Spec.Mounts,
			monRootfs:     rootfsParams.MonRootfs,
			mountedPath:   rootfsParams.MountedPath,
			path:          rootfsParams.Path,
			kernelPath:    unikernelPath,
			initrdPath:    initrdPath,
			uruncJSONPath: uruncJSONFilename,
			guestType:     unikernelType,
			guest:         unikernel,
		}
	case "initrd":
		rfsBuilder = initrdRootfs{
			mounts:             u.Spec.Mounts,
			initrdHostFullPath: filepath.Join(rootfsParams.MonRootfs, rootfsParams.Path),
			monRootfs:          rootfsParams.MonRootfs,
		}
	case "virtiofs", "9pfs":
		rfsBuilder = sharedfsRootfs{
			mounts:      u.Spec.Mounts,
			monRootfs:   rootfsParams.MonRootfs,
			mountedPath: rootfsParams.MountedPath,
			sfsType:     rootfsParams.Type,
			vfsdConfig:  virtiofsdConfig,
			sharedPath:  containerRootfsMountPath,
			memory:      vmmArgs.MemSizeB,
		}
		vmmArgs.UnikernelPath = adjustPathsForSharedfs(vmmArgs.UnikernelPath)
		vmmArgs.InitrdPath = adjustPathsForSharedfs(vmmArgs.InitrdPath)
	default:
		uniklog.Debug("No rootfs for guest")
		rfsBuilder = noRootfs{
			monRootfs:            rootfsParams.MonRootfs,
			annotBlockPath:       u.State.Annotations[annotBlock],
			annotBlockMountPoint: u.State.Annotations[annotBlockMntPoint],
		}
	}

	if err := rfsBuilder.preSetup(); err != nil {
		return nil, types.RootfsParams{}, fmt.Errorf("setupRootfs: preSetup: %w", err)
	}
	if err := prepareRoot(rootfsParams.MonRootfs, u.Spec.Linux.RootfsPropagation); err != nil {
		return nil, types.RootfsParams{}, fmt.Errorf("setupRootfs: prepareRoot: %w", err)
	}
	if err := prepareMonRootfs(rootfsParams.MonRootfs, vmm.Path(), u.UruncCfg.Monitors[vmmType].DataPath, vmm.UsesKVM(), withTUNTAP); err != nil {
		return nil, types.RootfsParams{}, fmt.Errorf("setupRootfs: prepareMonRootfs: %w", err)
	}
	if err := rfsBuilder.postSetup(); err != nil {
		return nil, types.RootfsParams{}, fmt.Errorf("setupRootfs: postSetup: %w", err)
	}

	return rfsBuilder, rootfsParams, nil
}

// setupVAccel configures vAccel acceleration if annotations are present.
// It mutates vmmArgs and unikernelParams in place.
func (u *Unikontainer) setupVAccel(
	vmmArgs *types.ExecArgs,
	unikernelParams *types.UnikernelParams,
	monRootfs string,
) {
	vAccelType, vsockSocketPath, rpcAddress, err := resolveVAccelConfig(
		u.State.Annotations[annotHypervisor],
		u.Spec.Annotations,
	)
	if err != nil {
		uniklog.Debugf("vAccel config: %v", err)
		return
	}
	if vAccelType != "vsock" {
		return
	}

	for i, envVar := range unikernelParams.EnvVars {
		if strings.HasPrefix(envVar, "VACCEL_RPC_ADDRESS=") {
			unikernelParams.EnvVars = remove(unikernelParams.EnvVars, i)
			break
		}
	}
	unikernelParams.EnvVars = append(unikernelParams.EnvVars, "VACCEL_RPC_ADDRESS="+rpcAddress)

	if err := prepareVSockEnvironment(monRootfs, u.State.Annotations[annotHypervisor], vsockSocketPath); err != nil {
		uniklog.Debugf("failed to prepare vsock mounts: %v", err)
	}

	vmmArgs.VAccelType = vAccelType
	vmmArgs.VSockDevPath = vsockSocketPath
	vmmArgs.VSockDevID = idToGuestCID(u.State.ID)
}
