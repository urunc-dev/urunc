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
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/internal/constants"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

func containerBootSpec(extra map[string]string) *specs.Spec {
	annot := map[string]string{
		annotBootKernel: "/opt/urunc/boot/bzImage",
		annotBootInitrd: "/opt/urunc/boot/container-initrd",
	}
	for k, v := range extra {
		annot[k] = v
	}
	return &specs.Spec{Annotations: annot}
}

func TestContainerBootAnnotations(t *testing.T) {
	t.Parallel()

	t.Run("the boot annotations alone describe a Linux guest on qemu", func(t *testing.T) {
		t.Parallel()
		config := getConfigFromSpec(containerBootSpec(nil))

		assert.Equal(t, unikernels.LinuxUnikernel, config.UnikernelType)
		assert.Equal(t, string(hypervisors.QemuVmm), config.Hypervisor)
		assert.Equal(t, "true", config.MountRootfs)
		assert.Empty(t, config.UnikernelBinary)
		require.NoError(t, config.validate(), "binary must not be required with a host kernel")
		require.NoError(t, config.validateValues())

		// The boot annotations must reach the persisted state through Map.
		m := config.Map()
		assert.Equal(t, "/opt/urunc/boot/bzImage", m[annotBootKernel])
		assert.Equal(t, "/opt/urunc/boot/container-initrd", m[annotBootInitrd])
	})

	t.Run("explicit matching annotations are accepted", func(t *testing.T) {
		t.Parallel()
		config := getConfigFromSpec(containerBootSpec(map[string]string{
			annotType:        unikernels.LinuxUnikernel,
			annotHypervisor:  string(hypervisors.QemuVmm),
			annotMountRootfs: "true",
		}))
		require.NoError(t, config.validate())
		require.NoError(t, config.validateValues())
	})

	t.Run("firecracker is accepted (block rootfs)", func(t *testing.T) {
		t.Parallel()
		config := getConfigFromSpec(containerBootSpec(map[string]string{
			annotType:        unikernels.LinuxUnikernel,
			annotHypervisor:  string(hypervisors.FirecrackerVmm),
			annotMountRootfs: "true",
		}))
		require.NoError(t, config.validate())
		require.NoError(t, config.validateValues())
	})

	rejected := []struct {
		name  string
		extra map[string]string
		want  string
	}{
		{"a relative kernel path", map[string]string{annotBootKernel: "boot/bzImage"}, "absolute host path"},
		{"a relative initrd path", map[string]string{annotBootInitrd: "container-initrd"}, "absolute host path"},
		{"an unclean kernel path", map[string]string{annotBootKernel: "/opt/../boot/bzImage"}, "clean path"},
		{"a kernel path with unsafe characters", map[string]string{annotBootKernel: "/opt/boot/bz,Image"}, "must match"},
		{"a unikernel binary as well", map[string]string{annotBinary: "/unikernel/app"}, "mutually exclusive"},
		{"a guest initrd as well", map[string]string{annotInitrd: "/initrd.img"}, "mutually exclusive"},
		{"a block image as well", map[string]string{annotBlock: "/rootfs.img", annotBlockMntPoint: "/"}, "block image"},
		{"a non-linux guest", map[string]string{annotType: unikernels.UnikraftUnikernel}, annotType},
		{"an unsupported monitor", map[string]string{annotHypervisor: string(hypervisors.CloudHypervisorVmm)}, annotHypervisor},
		{"mountRootfs disabled", map[string]string{annotMountRootfs: "false"}, annotMountRootfs},
	}
	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			config := getConfigFromSpec(containerBootSpec(tc.extra))
			err := config.validateValues()
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.want)
		})
	}

	t.Run("rejects a lone boot annotation", func(t *testing.T) {
		t.Parallel()
		for _, key := range []string{annotBootKernel, annotBootInitrd} {
			config := getConfigFromSpec(&specs.Spec{Annotations: map[string]string{key: "/opt/urunc/boot/file"}})
			// A lone boot annotation still marks the image as a urunc workload
			// (so it is not silently handed to the default runtime) and is then
			// rejected with a clear message by the value validation.
			assert.Equal(t, unikernels.LinuxUnikernel, config.UnikernelType)
			require.NoError(t, config.validate())
			assert.ErrorContains(t, config.validateValues(), "must be set together")
		}
	})

	t.Run("boot annotations never come from urunc.json", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		data := `{"com.urunc.unikernel.unikernelType":"bGludXg=","com.urunc.unikernel.hypervisor":"cWVtdQ==",` +
			`"com.urunc.unikernel.binary":"L3VuaWtlcm5lbA==","com.urunc.unikernel.bootKernel":"L2hvc3Qva2VybmVs",` +
			`"com.urunc.unikernel.bootInitrd":"L2hvc3QvaW5pdHJk","com.urunc.unikernel.mountRootfs":"dHJ1ZQ=="}`
		path := filepath.Join(dir, uruncJSONFilename)
		require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

		config, err := getConfigFromJSON(path)
		require.NoError(t, err)
		assert.Empty(t, config.BootKernel)
		assert.Empty(t, config.BootInitrd)
	})
}

func TestSharedfsRootfsContainerBoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	kernel := filepath.Join(dir, "bzImage")
	bootInitrd := filepath.Join(dir, "container-initrd")
	require.NoError(t, os.WriteFile(kernel, []byte("kernel"), 0o644))
	require.NoError(t, os.WriteFile(bootInitrd, []byte("initrd"), 0o644))

	s := sharedfsRootfs{
		mountedPath:    dir,
		sfsType:        "virtiofs",
		vfsdConfig:     types.ExtraBinConfig{Path: "/usr/libexec/virtiofsd"},
		sharedPath:     containerRootfsMountPath,
		bootKernelHost: kernel,
		bootInitrdHost: bootInitrd,
	}

	t.Run("boot files are mounted read-only into the monitor rootfs", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, s.preSetup())

		mounts, err := s.getMounts()
		require.NoError(t, err)

		byDest := map[string]specs.Mount{}
		for _, m := range mounts {
			byDest[m.Destination] = m
		}
		for dest, src := range map[string]string{
			constants.ContainerBootKernelPath: kernel,
			constants.ContainerBootInitrdPath: bootInitrd,
		} {
			m, ok := byDest[dest]
			require.True(t, ok, "expected a mount at %s", dest)
			assert.Equal(t, "bind", m.Type)
			assert.Equal(t, src, m.Source)
			assert.Contains(t, m.Options, "ro", "the host boot files are shared by every container and must not be written")
			assert.Contains(t, m.Options, "bind")
		}
		// The boot files live next to the shared container rootfs, never inside it.
		for dest := range byDest {
			if dest == constants.ContainerBootKernelPath || dest == constants.ContainerBootInitrdPath {
				assert.NotContains(t, dest, containerRootfsMountPath)
			}
		}
	})

	t.Run("a missing boot file fails at create", func(t *testing.T) {
		t.Parallel()
		missing := s
		missing.bootInitrdHost = filepath.Join(dir, "no-such-initrd")
		err := missing.preSetup()
		require.Error(t, err)
		assert.ErrorContains(t, err, annotBootInitrd)
	})

	t.Run("a directory is not a boot file", func(t *testing.T) {
		t.Parallel()
		notFile := s
		notFile.bootKernelHost = dir
		err := notFile.preSetup()
		require.Error(t, err)
		assert.ErrorContains(t, err, "regular file")
	})

	t.Run("without boot annotations nothing changes", func(t *testing.T) {
		t.Parallel()
		plain := sharedfsRootfs{mountedPath: dir, sfsType: "virtiofs", vfsdConfig: types.ExtraBinConfig{Path: "/usr/libexec/virtiofsd"}, sharedPath: containerRootfsMountPath}
		require.NoError(t, plain.preSetup())
		mounts, err := plain.getMounts()
		require.NoError(t, err)
		for _, m := range mounts {
			assert.NotEqual(t, constants.ContainerBootKernelPath, m.Destination)
			assert.NotEqual(t, constants.ContainerBootInitrdPath, m.Destination)
		}
	})
}

func TestContainerBootSelectorSkipsBlock(t *testing.T) {
	t.Parallel()

	rs := &rootfsSelector{annot: map[string]string{
		annotBootKernel: "/opt/urunc/boot/bzImage",
		annotBootInitrd: "/opt/urunc/boot/container-initrd",
	}}
	assert.True(t, rs.hasContainerBoot())

	rs = &rootfsSelector{annot: map[string]string{annotBootInitrd: "/opt/urunc/boot/container-initrd"}}
	assert.False(t, rs.hasContainerBoot(), "both boot annotations are needed")

	// The host boot initrd is not the guest rootfs initrd.
	_, ok := rs.tryInitrd()
	assert.False(t, ok)
}
