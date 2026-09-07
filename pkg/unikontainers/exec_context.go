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

// Platform-neutral prologue of the container launch: turn an OCI bundle (spec +
// urunc annotations + urunc config) into the monitor, the unikernel, and the
// base ExecArgs/UnikernelParams they need. This is the part of the Linux Exec
// flow that has no platform dependency; the Linux engine and the darwin runner
// share it here instead of each re-deriving it. Everything ordered around
// platform setup (networking, rootfs realization/mount namespaces, vAccel, the
// unikernel Init/CommandString, and the final monitor handoff) stays with the
// caller.

package unikontainers

import (
	"fmt"
	"os"
	"path/filepath"

	specs "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// execContext holds the platform-neutral results of parsing an OCI bundle into
// the arguments the monitor and unikernel need. The caller fills in the
// platform-specific remainder (network, rootfs realization, vAccel) before
// Init'ing the unikernel and handing off to the monitor.
type execContext struct {
	VMM             types.VMM
	Unikernel       types.Unikernel
	VMMArgs         types.ExecArgs
	UnikernelParams types.UnikernelParams
	VMMType         string
	RootfsDir       string
	UnikernelPath   string
	InitrdPath      string
	VirtiofsdConfig types.ExtraBinConfig
}

// monitorFamily maps a urunc hypervisor annotation value to the monitor family
// the unikernel builders switch on ("qemu", "firecracker", "cloud-hypervisor").
// It normalizes the darwin QEMU/HVF alias ("qemu-hvf") to "qemu"; every other
// value (including all Linux monitors) passes through unchanged, so this is a
// no-op on Linux.
func monitorFamily(vmmType string) string {
	switch vmmType {
	case "qemu-hvf":
		return "qemu"
	default:
		return vmmType
	}
}

// buildExecContext performs the platform-neutral prologue of a container
// launch. annot is the (decoded) urunc annotation map, containerID the sandbox
// id, bundle the bundle directory, and cfg the urunc config. It resolves the
// rootfs directory, constructs the monitor and unikernel from the annotations,
// and builds the base ExecArgs/UnikernelParams from the OCI spec and config.
func buildExecContext(spec *specs.Spec, annot map[string]string, containerID, bundle string, cfg *UruncConfig) (*execContext, error) {
	if cfg == nil {
		return nil, fmt.Errorf("urunc config is required")
	}

	bundleDir := filepath.Clean(bundle)
	specRoot := "rootfs"
	if spec.Root != nil && spec.Root.Path != "" {
		specRoot = spec.Root.Path
	}
	rootfsDir, err := resolveAgainstBase(bundleDir, filepath.Clean(specRoot))
	if err != nil {
		return nil, fmt.Errorf("could not resolve rootfs directory: %w", err)
	}

	unikernelType := annot[annotType]
	unikernel, err := unikernels.New(unikernelType)
	if err != nil {
		return nil, err
	}

	vmmType := annot[annotHypervisor]
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(vmmType), cfg.Monitors)
	if err != nil {
		return nil, err
	}

	unikernelVersion := annot[annotVersion]
	unikernelPath := annot[annotBinary]
	initrdPath := annot[annotInitrd]

	defaultVCPUs := cfg.Monitors[vmmType].DefaultVCPUs
	if defaultVCPUs < 1 {
		defaultVCPUs = 1
	}
	defaultMemSizeMB := cfg.Monitors[vmmType].DefaultMemoryMB

	vmmArgs := types.ExecArgs{
		ContainerID:   containerID,
		UnikernelPath: unikernelPath,
		InitrdPath:    initrdPath,
		Seccomp:       true, // Enable Seccomp by default
		MemSizeB:      uint64(defaultMemSizeMB * 1024 * 1024),
		VCPUs:         uint(defaultVCPUs),
		Environment:   os.Environ(),
	}

	// Spec.Linux is a Linux-only section and is nil on darwin. On Linux, honor
	// a memory limit and disable seccomp when the container is unconfined; on
	// darwin there is no seccomp.
	if spec.Linux != nil {
		if spec.Linux.Resources != nil && spec.Linux.Resources.Memory != nil &&
			spec.Linux.Resources.Memory.Limit != nil && *spec.Linux.Resources.Memory.Limit > 0 {
			vmmArgs.MemSizeB = uint64(*spec.Linux.Resources.Memory.Limit) // nolint:gosec
		}
		if spec.Linux.Seccomp == nil {
			vmmArgs.Seccomp = false
		}
	} else {
		vmmArgs.Seccomp = false
	}

	var procAttrs types.ProcessConfig
	var cmdline []string
	var envVars []string
	if spec.Process != nil {
		procAttrs = types.ProcessConfig{
			UID:     spec.Process.User.UID,
			GID:     spec.Process.User.GID,
			WorkDir: spec.Process.Cwd,
		}
		cmdline = spec.Process.Args
		envVars = spec.Process.Env
	}

	unikernelParams := types.UnikernelParams{
		CmdLine:  cmdline,
		EnvVars:  envVars,
		Monitor:  monitorFamily(vmmType),
		Version:  unikernelVersion,
		ProcConf: procAttrs,
	}

	return &execContext{
		VMM:             vmm,
		Unikernel:       unikernel,
		VMMArgs:         vmmArgs,
		UnikernelParams: unikernelParams,
		VMMType:         vmmType,
		RootfsDir:       rootfsDir,
		UnikernelPath:   unikernelPath,
		InitrdPath:      initrdPath,
		VirtiofsdConfig: cfg.ExtraBins["virtiofsd"],
	}, nil
}

// annotMapFromConfig rebuilds the internal urunc annotation map from a decoded
// UnikernelConfig. GetUnikernelConfig applies the urunc.json fallback and
// base64 decoding; this hands the decoded values back to the annotation-keyed
// prologue so a runner outside this package need not know the annotation keys.
func annotMapFromConfig(c *UnikernelConfig) map[string]string {
	return map[string]string{
		annotType:          c.UnikernelType,
		annotVersion:       c.UnikernelVersion,
		annotBinary:        c.UnikernelBinary,
		annotHypervisor:    c.Hypervisor,
		annotInitrd:        c.Initrd,
		annotBlock:         c.Block,
		annotBlockMntPoint: c.BlkMntPoint,
		annotMountRootfs:   c.MountRootfs,
	}
}

// ExecContext is the exported, platform-neutral launch context for runners
// outside this package — the darwin cmd/urunc, which does not drive the full
// Linux Unikontainer lifecycle but still wants to build the monitor, unikernel,
// arguments and rootfs choice through urunc's own code rather than re-deriving
// them.
type ExecContext struct {
	VMM             types.VMM
	Unikernel       types.Unikernel
	VMMArgs         types.ExecArgs
	UnikernelParams types.UnikernelParams
	Rootfs          types.RootfsParams
	VMMType         string
	RootfsDir       string
	UnikernelPath   string
	InitrdPath      string
}

// PrepareExec runs the platform-neutral launch prologue for a bundle: it reads
// the urunc config (annotations with urunc.json fallback + decoding), builds
// the monitor/unikernel and base ExecArgs/UnikernelParams, and chooses the
// guest rootfs — the same sequence the Linux Exec performs before its
// platform-specific networking and rootfs realization. The caller (darwin)
// then realizes the rootfs share, Init's the unikernel and launches the
// monitor.
func PrepareExec(spec *specs.Spec, bundle, containerID string, cfg *UruncConfig) (*ExecContext, error) {
	config, err := GetUnikernelConfig(bundle, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to read urunc config: %w", err)
	}
	annot := annotMapFromConfig(config)

	ec, err := buildExecContext(spec, annot, containerID, bundle, cfg)
	if err != nil {
		return nil, err
	}

	specRoot := "rootfs"
	if spec.Root != nil && spec.Root.Path != "" {
		specRoot = spec.Root.Path
	}
	rootfs, err := ChooseRootfs(bundle, specRoot, annot, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to select guest rootfs: %w", err)
	}

	return &ExecContext{
		VMM:             ec.VMM,
		Unikernel:       ec.Unikernel,
		VMMArgs:         ec.VMMArgs,
		UnikernelParams: ec.UnikernelParams,
		Rootfs:          rootfs,
		VMMType:         ec.VMMType,
		RootfsDir:       ec.RootfsDir,
		UnikernelPath:   ec.UnikernelPath,
		InitrdPath:      ec.InitrdPath,
	}, nil
}
