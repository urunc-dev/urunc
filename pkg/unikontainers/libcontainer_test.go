//go:build cgo

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
	"path/filepath"
	"testing"

	devices "github.com/opencontainers/cgroups/devices/config"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// newTestUnikontainer builds the minimum Unikontainer that BuildContainerConfig
// needs: a spec and a monitor resources file. It returns the chosen rootfs
// params so the caller can pass them to BuildContainerConfig. The monitor rootfs
// is a real directory, because specconv resolves it and libcontainer's
// validation insists on an existing absolute path.
func newTestUnikontainer(t *testing.T, spec *specs.Spec, monRes monitorResources) (*Unikontainer, types.RootfsParams) {
	t.Helper()

	baseDir := t.TempDir()
	monRootfs := t.TempDir()

	rootfsParams := newRootfsResult("initrd", "initrd.cpio", "", monRootfs)
	monRes.Rootfs = rootfsParams

	err := saveMonitorResources(baseDir, monRes)
	require.NoError(t, err)

	u := &Unikontainer{
		BaseDir: baseDir,
		RootDir: filepath.Dir(baseDir),
		Spec:    spec,
		State: &specs.State{
			ID: "test-container",
		},
		UruncCfg: defaultUruncConfig(),
	}

	return u, rootfsParams
}

// testSpec returns a spec close to what a real container carries: a couple of
// namespaces, a capability set and a cgroup path.
func testSpec() *specs.Spec {
	return &specs.Spec{
		Version: specs.Version,
		Root:    &specs.Root{Path: "rootfs", Readonly: true},
		Process: &specs.Process{
			Cwd:  "/guest/workdir",
			Args: []string{"/unikernel"},
			Capabilities: &specs.LinuxCapabilities{
				Bounding:    []string{"CAP_CHOWN", "CAP_KILL"},
				Effective:   []string{"CAP_CHOWN"},
				Permitted:   []string{"CAP_CHOWN", "CAP_KILL"},
				Inheritable: []string{"CAP_CHOWN"},
				Ambient:     []string{"CAP_CHOWN"},
			},
		},
		Linux: &specs.Linux{
			CgroupsPath: "/pods/test-container",
			Namespaces: []specs.LinuxNamespace{
				{Type: specs.NetworkNamespace, Path: "/proc/1234/ns/net"},
				{Type: specs.MountNamespace},
				{Type: specs.UTSNamespace},
			},
		},
	}
}

// countString returns how many times want appears in values.
func countString(values []string, want string) int {
	count := 0
	for _, v := range values {
		if v == want {
			count++
		}
	}

	return count
}

func TestBuildContainerConfig(t *testing.T) {
	t.Run("roots the monitor at the monitor rootfs", func(t *testing.T) {
		t.Parallel()
		spec := testSpec()
		u, rootfsParams := newTestUnikontainer(t, spec, monitorResources{})

		config, err := u.BuildContainerConfig(false)
		require.NoError(t, err)

		assert.Equal(t, rootfsParams.MonRootfs, config.Rootfs)
		// The container's rootfs is read-only, the monitor's is not.
		assert.False(t, config.Readonlyfs)
		assert.False(t, config.NoPivotRoot)
		// The container's spec must not have been modified.
		assert.Equal(t, "rootfs", spec.Root.Path)
		assert.True(t, spec.Root.Readonly)
		assert.Equal(t, "/guest/workdir", spec.Process.Cwd)
	})

	t.Run("maps the namespaces from the spec", func(t *testing.T) {
		t.Parallel()
		u, _ := newTestUnikontainer(t, testSpec(), monitorResources{})

		config, err := u.BuildContainerConfig(false)
		require.NoError(t, err)

		assert.True(t, config.Namespaces.Contains(configs.NEWNET))
		assert.True(t, config.Namespaces.Contains(configs.NEWNS))
		assert.True(t, config.Namespaces.Contains(configs.NEWUTS))
		assert.False(t, config.Namespaces.Contains(configs.NEWPID))
		assert.False(t, config.Namespaces.Contains(configs.NEWUSER))
		assert.Equal(t, "/proc/1234/ns/net", config.Namespaces.PathOf(configs.NEWNET))
	})

	t.Run("carries the monitor mounts and devices", func(t *testing.T) {
		t.Parallel()
		monRes := monitorResources{
			Mounts: []specs.Mount{
				tmpfsMount("/tmp", "65536k"),
				bindMount("/usr/bin/qemu-system-x86_64", "/usr/bin/qemu-system-x86_64", true),
			},
			Devices: []specs.LinuxDevice{
				{Path: "/dev/kvm", Type: "c", Major: 10, Minor: 232},
				{Path: "/dev/null", Type: "c", Major: 1, Minor: 3},
			},
		}
		u, _ := newTestUnikontainer(t, testSpec(), monRes)

		config, err := u.BuildContainerConfig(false)
		require.NoError(t, err)

		require.Len(t, config.Mounts, 2)
		assert.Equal(t, "/tmp", config.Mounts[0].Destination)
		assert.Equal(t, "/usr/bin/qemu-system-x86_64", config.Mounts[1].Destination)

		// libcontainer adds the standard device nodes on top of the ones urunc
		// gathered, and drops its own entry for a device the spec already
		// declares, so /dev/null must appear exactly once.
		paths := make([]string, 0, len(config.Devices))
		for _, dev := range config.Devices {
			paths = append(paths, dev.Path)
		}
		assert.Contains(t, paths, "/dev/kvm")
		assert.Contains(t, paths, "/dev/zero")
		assert.Equal(t, 1, countString(paths, "/dev/null"))
	})

	t.Run("places the monitor in the container cgroup without limits", func(t *testing.T) {
		t.Parallel()
		spec := testSpec()
		limit := int64(64 * 1024 * 1024)
		spec.Linux.Resources = &specs.LinuxResources{
			Memory: &specs.LinuxMemory{Limit: &limit},
		}
		u, _ := newTestUnikontainer(t, spec, monitorResources{})

		config, err := u.BuildContainerConfig(false)
		require.NoError(t, err)

		assert.Equal(t, "/pods/test-container", config.Cgroups.Path)
		assert.False(t, config.Cgroups.Systemd)

		// Only the allow-all device rule, and in particular no memory limit:
		// the guest's RAM is sized from the same limit, so applying it to the
		// monitor's cgroup would OOM-kill the VMM.
		require.NotNil(t, config.Cgroups.Resources)
		assert.Zero(t, config.Cgroups.Resources.Memory)
		assert.Zero(t, config.Cgroups.Resources.CpuQuota)
		require.Len(t, config.Cgroups.Resources.Devices, 1)
		assert.Equal(t, devices.WildcardDevice, config.Cgroups.Resources.Devices[0].Type)
		assert.True(t, config.Cgroups.Resources.Devices[0].Allow)
	})

	t.Run("honors the systemd cgroup driver", func(t *testing.T) {
		t.Parallel()
		spec := testSpec()
		spec.Linux.CgroupsPath = "system.slice:urunc:test-container"
		u, _ := newTestUnikontainer(t, spec, monitorResources{})

		config, err := u.BuildContainerConfig(true)
		require.NoError(t, err)

		assert.True(t, config.Cgroups.Systemd)
		assert.Equal(t, "system.slice", config.Cgroups.Parent)
		assert.Equal(t, "urunc", config.Cgroups.ScopePrefix)
		assert.Equal(t, "test-container", config.Cgroups.Name)
	})

	t.Run("does not register the poststart and poststop hooks", func(t *testing.T) {
		t.Parallel()
		spec := testSpec()
		hook := specs.Hook{Path: "/bin/true"}
		spec.Hooks = &specs.Hooks{
			Prestart:        []specs.Hook{hook},
			CreateRuntime:   []specs.Hook{hook},
			CreateContainer: []specs.Hook{hook},
			StartContainer:  []specs.Hook{hook},
			Poststart:       []specs.Hook{hook},
			Poststop:        []specs.Hook{hook},
		}
		u, _ := newTestUnikontainer(t, spec, monitorResources{})

		config, err := u.BuildContainerConfig(false)
		require.NoError(t, err)

		// urunc keeps its own timing for these two, since libcontainer would run
		// Poststart from container.Start(), i.e. during create.
		assert.False(t, config.HasHook(configs.Poststart))
		assert.False(t, config.HasHook(configs.Poststop))
		// The rest are libcontainer's to run.
		assert.True(t, config.HasHook(configs.Prestart))
		assert.True(t, config.HasHook(configs.CreateRuntime))
		assert.True(t, config.HasHook(configs.CreateContainer))
		assert.True(t, config.HasHook(configs.StartContainer))
	})

	// urunc delegates syscall filtering to the monitor and is never built with
	// the seccomp build tag, so a non-nil Seccomp here would fail libcontainer's
	// init. This guards against a specconv bump re-introducing it.
	t.Run("never carries a seccomp profile", func(t *testing.T) {
		t.Parallel()
		spec := testSpec()
		spec.Linux.Seccomp = &specs.LinuxSeccomp{
			DefaultAction: specs.ActErrno,
			Architectures: []specs.Arch{specs.ArchX86_64},
			Syscalls: []specs.LinuxSyscall{
				{Names: []string{"read", "write"}, Action: specs.ActAllow},
			},
		}
		u, _ := newTestUnikontainer(t, spec, monitorResources{})

		config, err := u.BuildContainerConfig(false)
		require.NoError(t, err)

		assert.Nil(t, config.Seccomp)
	})
}

func TestMonitorCapabilities(t *testing.T) {
	t.Run("adds CAP_NET_ADMIN to all five sets", func(t *testing.T) {
		t.Parallel()
		spec := testSpec()

		caps := MonitorCapabilities(spec)

		require.NotNil(t, caps)
		for name, set := range map[string][]string{
			"bounding":    caps.Bounding,
			"effective":   caps.Effective,
			"permitted":   caps.Permitted,
			"inheritable": caps.Inheritable,
			"ambient":     caps.Ambient,
		} {
			assert.Contains(t, set, monCaps[0], "%s must carry CAP_NET_ADMIN", name)
			assert.Contains(t, set, "CAP_CHOWN", "%s must keep the container's own capabilities", name)
		}

		// The container's spec must not have been touched.
		assert.NotContains(t, spec.Process.Capabilities.Bounding, monCaps[0])
	})

	t.Run("does not duplicate a CAP_NET_ADMIN already present", func(t *testing.T) {
		t.Parallel()
		spec := testSpec()
		spec.Process.Capabilities.Bounding = []string{monCaps[0]}

		caps := MonitorCapabilities(spec)

		require.NotNil(t, caps)
		assert.Equal(t, []string{monCaps[0]}, caps.Bounding)
	})

	t.Run("a spec without capabilities gives nil", func(t *testing.T) {
		t.Parallel()
		spec := testSpec()
		spec.Process.Capabilities = nil

		assert.Nil(t, MonitorCapabilities(spec))
	})
}
