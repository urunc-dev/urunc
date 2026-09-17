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

package unikernels

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestLinuxContainerBootCmdLine(t *testing.T) {
	t.Parallel()

	l := &Linux{}
	require.NoError(t, l.parseContainerBootCmdLine([]string{"/bin/sh", "-c", "printf container-boot-ok"}))
	assert.Equal(t, containerBootInit, l.App, "the boot initrd's /init is the guest init")
	assert.Equal(t, "/bin/sh -c 'printf container-boot-ok'", l.Command, "the whole container command is handed to /init")

	err := l.parseContainerBootCmdLine(nil)
	assert.Error(t, err, "a container boot needs a command")
}

func TestLinuxContainerBootCommandString(t *testing.T) {
	t.Parallel()

	l := &Linux{
		App:           containerBootInit,
		Command:       "/bin/sh -c 'printf container-boot-ok'",
		Monitor:       "qemu",
		Env:           []string{"TOKEN=not-on-the-kernel-command-line"},
		RootFsType:    "virtiofs",
		InitrdConf:    true,
		ContainerBoot: true,
	}

	cmdline, err := l.CommandString()
	require.NoError(t, err)

	// The root parameters stay: /init mounts the share they describe itself.
	assert.Contains(t, cmdline, "root=fs0 rw rootfstype=virtiofs")
	assert.Contains(t, cmdline, "rdinit=/init -- /bin/sh -c 'printf container-boot-ok'")
	assert.Contains(t, cmdline, "console=hvc0")
	// The environment travels in the urunit configuration inside the initrd,
	// which /init points urunit at, so neither it nor a config path is here.
	assert.NotContains(t, cmdline, "TOKEN=")
	assert.NotContains(t, cmdline, "URUNIT_CONFIG")
	assert.NotContains(t, cmdline, "retain_initrd")

	// The urunit configuration is appended to the boot initrd, not handed to
	// the monitor as an initrd of its own.
	assert.Empty(t, l.MonitorCli().ExtraInitrd)
	assert.Contains(t, l.MonitorCli().OtherArgs, "-nodefaults")

	l.RootFsType = "9pfs"
	cmdline, err = l.CommandString()
	require.NoError(t, err)
	assert.Contains(t, cmdline, "root=fs0 rw rootfstype=9p rootflags=trans=virtio")
}

func TestLinuxContainerBootUrunitConfig(t *testing.T) {
	t.Parallel()

	// A container boot uses the very same urunit configuration as a urunit
	// image: environment, user and working directory, and no block volumes.
	l := &Linux{
		Env:           []string{"PATH=/usr/bin", "FOO=bar"},
		ProcConfig:    types.ProcessConfig{UID: 1000, GID: 1000, WorkDir: "/work"},
		ContainerBoot: true,
	}
	assert.Equal(t, "UES\nPATH=/usr/bin\nFOO=bar\nUEE\nUCS\nUID:1000\nGID:1000\nWD:/work\nUCE\nUBS\nUBE\n", l.buildUrunitConfig())
}

func TestLinuxUrunitConfAsInitrd(t *testing.T) {
	t.Parallel()

	// A urunit image on a shared rootfs gets its config as the initrd.
	assert.True(t, (&Linux{InitrdConf: true, RootFsType: "virtiofs"}).urunitConfAsInitrd())
	// An initrd rootfs carries the config inside the initrd.
	assert.False(t, (&Linux{InitrdConf: true, RootFsType: "initrd"}).urunitConfAsInitrd())
	// A container boot appends the config to its boot initrd.
	assert.False(t, (&Linux{InitrdConf: true, RootFsType: "virtiofs", ContainerBoot: true}).urunitConfAsInitrd())
}

func TestLinuxContainerBootInitRequiresInitrd(t *testing.T) {
	t.Parallel()

	l := &Linux{}
	err := l.Init(types.UnikernelParams{
		CmdLine:       []string{"/bin/true"},
		Monitor:       "qemu",
		Rootfs:        types.RootfsParams{Type: "virtiofs"},
		ContainerBoot: true,
	})
	require.Error(t, err, "a container boot without the boot initrd path cannot build the guest initrd")
	assert.ErrorContains(t, err, "boot initrd")
}
