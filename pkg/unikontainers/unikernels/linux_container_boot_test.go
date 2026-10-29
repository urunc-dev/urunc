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
	"strings"
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

func TestLinuxCommandStringVerbosity(t *testing.T) {
	t.Parallel()

	base := Linux{
		App:        containerBootInit,
		Command:    "/bin/sh",
		Monitor:    "qemu",
		RootFsType: "virtiofs",
	}

	quiet := base
	quiet.Verbose = false
	cmdline, err := quiet.CommandString()
	require.NoError(t, err)
	assert.Contains(t, cmdline, "quiet", "a non-debug log level boots the guest quietly")

	verbose := base
	verbose.Verbose = true
	cmdline, err = verbose.CommandString()
	require.NoError(t, err)
	assert.NotContains(t, cmdline, "quiet", "a debug log level leaves the guest verbose")
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
	assert.Equal(t, "UES\x00PATH=/usr/bin\x00FOO=bar\x00UEE\x00UCS\x00UID:1000\x00GID:1000\x00WD:/work\x00UCE\x00UBS\x00UBE\x00", l.buildUrunitConfig())
}

// The kernel command line drops quotes and empty arguments and stops at a
// "--", so a container boot's command travels in urunit's configuration, one
// record per argument, and the command line carries none of it.
func TestLinuxContainerBootArgsInConfig(t *testing.T) {
	t.Parallel()

	cmd := []string{"/bin/sh", "-c", `printf "<%s>\n" "a b" ''`, "--", "", "two\nlines"}
	l := &Linux{Monitor: "qemu", RootFsType: "virtiofs", ContainerBoot: true}
	require.NoError(t, l.parseContainerBootCmdLine(cmd))
	assert.Equal(t, cmd, l.args)
	l.InitrdConf = true
	l.ProcConfig = types.ProcessConfig{UID: 0, GID: 0, WorkDir: "/", Args: l.args}

	cmdline, err := l.CommandString()
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(cmdline, "rdinit=/init"), "nothing follows the init: %s", cmdline)
	assert.NotContains(t, cmdline, " -- ")

	want := "UCS\x00UID:0\x00GID:0\x00WD:/\x00ARC:6\x00ARV:/bin/sh\x00ARV:-c\x00" +
		"ARV:printf \"<%s>\\n\" \"a b\" ''\x00ARV:--\x00ARV:\x00ARV:two\nlines\x00UCE\x00"
	assert.Contains(t, l.buildUrunitConfig(), want)
}

// Without a command for urunit, as for a Linux guest whose init is not
// urunit, the arguments stay on the kernel command line.
func TestLinuxArgsOnCmdlineWithoutUrunit(t *testing.T) {
	t.Parallel()

	l := &Linux{Monitor: "qemu", RootFsType: "block"}
	require.NoError(t, l.parseCmdLine([]string{"/sbin/init", "--foo"}))
	cmdline, err := l.CommandString()
	require.NoError(t, err)
	assert.Contains(t, cmdline, "init=/sbin/init -- --foo")
	assert.NotContains(t, l.buildUrunitConfig(), "ARC:")
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

// hull on macOS builds its container boot command lines here too. A virtiofs
// root is the /vz-init role's own virtiofs tag, so no root= goes with it; a
// block root names /dev/vda. Vz reboots on a reset, so its guest powers off
// at the end and only stops on a panic; hvi exits on either.
func TestLinuxDarwinContainerBoot(t *testing.T) {
	t.Parallel()

	vz := &Linux{
		Monitor:       "vz",
		RootFsType:    "virtiofs",
		ContainerBoot: true,
		InitrdConf:    true,
		Net:           LinuxNet{DHCP: true},
		ProcConfig:    types.ProcessConfig{Args: []string{"/bin/sh"}},
		hostOS:        "darwin",
	}
	cmdline, err := vz.CommandString()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(cmdline, "panic=0 "), cmdline)
	for _, want := range []string{"console=hvc0", "ip=dhcp", "URUNIT_POWEROFF=1", "rdinit=/vz-init"} {
		assert.Contains(t, strings.Fields(cmdline), want)
	}
	assert.NotContains(t, cmdline, "root=")
	assert.NotContains(t, cmdline, " -- ")

	hvi := &Linux{
		Monitor:       "hvi",
		RootFsType:    "block",
		RootFsFsType:  "ext4",
		ContainerBoot: true,
		InitrdConf:    true,
		Net: LinuxNet{Address: "192.168.64.2", Gateway: "192.168.64.1",
			Mask: "255.255.255.0", DNS: "192.168.64.1"},
		ProcConfig: types.ProcessConfig{Args: []string{"/bin/sh"}},
		hostOS:     "darwin",
	}
	cmdline, err = hvi.CommandString()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(cmdline, "panic=-1 "), cmdline)
	assert.Contains(t, cmdline, "root=/dev/vda rw rootfstype=ext4")
	assert.Contains(t, cmdline, "ip=192.168.64.2::192.168.64.1:255.255.255.0:urunc:eth0:off:192.168.64.1")
	assert.True(t, strings.HasSuffix(cmdline, " rdinit=/init"), cmdline)
	assert.NotContains(t, cmdline, "URUNIT_POWEROFF")

	// The same virtiofs boot on a Linux host keeps its root and /init.
	vz.hostOS = "linux"
	cmdline, err = vz.CommandString()
	require.NoError(t, err)
	assert.Contains(t, cmdline, "root=fs0 rw rootfstype=virtiofs")
	assert.Contains(t, strings.Fields(cmdline), "rdinit=/init")
}
