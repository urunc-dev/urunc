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
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestNewLinux(t *testing.T) {
	t.Parallel()
	l := newLinux()
	require.NotNil(t, l)
	assert.Equal(t, "", l.App)
	assert.Equal(t, "", l.Command)
	assert.False(t, l.InitrdConf)
}

func TestLinuxSupportsBlock(t *testing.T) {
	t.Parallel()
	l := newLinux()
	assert.True(t, l.SupportsBlock())
}

func TestLinuxSupportsFS(t *testing.T) {
	t.Parallel()
	l := newLinux()
	cases := []struct {
		fs   string
		want bool
	}{
		{"ext2", true},
		{"ext3", true},
		{"ext4", true},
		{"9pfs", true},
		{"virtiofs", true},
		{"xfs", false},
		{"btrfs", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.fs, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, l.SupportsFS(c.fs))
		})
	}
}

func TestLinuxMonitorNetCli(t *testing.T) {
	t.Parallel()
	l := newLinux()
	assert.Equal(t, "", l.MonitorNetCli("eth0", "00:11:22:33:44:55"))
}

func TestIsIPInSubnet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		net  LinuxNet
		want bool
	}{
		{
			name: "address inside /24 gateway subnet",
			net:  LinuxNet{Address: "192.168.1.10", Gateway: "192.168.1.1", Mask: "255.255.255.0"},
			want: true,
		},
		{
			name: "address outside /24 gateway subnet",
			net:  LinuxNet{Address: "10.0.0.5", Gateway: "192.168.1.1", Mask: "255.255.255.0"},
			want: false,
		},
		{
			name: "address inside /16 gateway subnet",
			net:  LinuxNet{Address: "172.16.5.10", Gateway: "172.16.0.1", Mask: "255.255.0.0"},
			want: true,
		},
		{
			name: "address outside /16 gateway subnet",
			net:  LinuxNet{Address: "172.17.0.5", Gateway: "172.16.0.1", Mask: "255.255.0.0"},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, IsIPInSubnet(c.net))
		})
	}
}

func TestLinuxParseCmdLine(t *testing.T) {
	t.Parallel()

	t.Run("empty cmdline returns error", func(t *testing.T) {
		t.Parallel()
		l := &Linux{}
		err := l.parseCmdLine(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no init")
	})

	t.Run("single argument sets App and empty Command", func(t *testing.T) {
		t.Parallel()
		l := &Linux{}
		err := l.parseCmdLine([]string{"/bin/sh"})
		require.NoError(t, err)
		assert.Equal(t, "/bin/sh", l.App)
		assert.Equal(t, "", l.Command)
	})

	t.Run("multiple single-word args build space-joined Command", func(t *testing.T) {
		t.Parallel()
		l := &Linux{}
		err := l.parseCmdLine([]string{"/bin/echo", "hello", "world"})
		require.NoError(t, err)
		assert.Equal(t, "/bin/echo", l.App)
		assert.Equal(t, "hello world", l.Command)
	})

	t.Run("multi-word arg gets wrapped in single quotes", func(t *testing.T) {
		t.Parallel()
		l := &Linux{}
		err := l.parseCmdLine([]string{"/bin/sh", "-c", "echo hi there"})
		require.NoError(t, err)
		assert.Equal(t, "/bin/sh", l.App)
		assert.Equal(t, "-c 'echo hi there'", l.Command)
	})

	t.Run("surrounding whitespace is trimmed before quoting check", func(t *testing.T) {
		t.Parallel()
		l := &Linux{}
		err := l.parseCmdLine([]string{"  /bin/sh  ", " -c "})
		require.NoError(t, err)
		assert.Equal(t, "/bin/sh", l.App)
		assert.Equal(t, "-c", l.Command)
	})
}

func TestLinuxConfigureNetwork(t *testing.T) {
	t.Parallel()
	l := &Linux{}
	l.configureNetwork(types.NetDevParams{
		IP:      "10.0.0.5",
		Gateway: "10.0.0.1",
		Mask:    "255.255.255.0",
		MAC:     "00:11:22:33:44:55",
		TapDev:  "tap0",
		MTU:     1500,
	})
	assert.Equal(t, "10.0.0.5", l.Net.Address)
	assert.Equal(t, "10.0.0.1", l.Net.Gateway)
	assert.Equal(t, "255.255.255.0", l.Net.Mask)
}

func TestLinuxBuildUrunitConfig(t *testing.T) {
	t.Parallel()

	t.Run("no env and no blocks emits empty marker pairs", func(t *testing.T) {
		t.Parallel()
		l := &Linux{ProcConfig: types.ProcessConfig{UID: 1000, GID: 100, WorkDir: "/app"}}
		out := l.buildUrunitConfig()
		assert.Contains(t, out, "UES\nUEE\n")
		assert.Contains(t, out, "UCS\n")
		assert.Contains(t, out, "UID:1000\n")
		assert.Contains(t, out, "GID:100\n")
		assert.Contains(t, out, "WD:/app\n")
		assert.Contains(t, out, "UCE\n")
		assert.Contains(t, out, "UBS\nUBE\n")
	})

	t.Run("env vars are joined inside UES/UEE markers", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Env: []string{"FOO=bar", "BAZ=qux"}}
		out := l.buildUrunitConfig()
		assert.Contains(t, out, "UES\nFOO=bar\nBAZ=qux\nUEE\n")
	})

	t.Run("rootfs block is skipped, others are emitted", func(t *testing.T) {
		t.Parallel()
		l := &Linux{
			Monitor: "qemu",
			Blk: []types.BlockDevParams{
				{ID: "rootfs", MountPoint: "/", Source: "/r"},
				{ID: "data", MountPoint: "/data", Source: "/d"},
			},
		}
		out := l.buildUrunitConfig()
		assert.NotContains(t, out, "ID:rootfs")
		assert.Contains(t, out, "ID:data\nMP:/data\n")
	})

	t.Run("firecracker prefixes block IDs with FC", func(t *testing.T) {
		t.Parallel()
		l := &Linux{
			Monitor: "firecracker",
			Blk:     []types.BlockDevParams{{ID: "data", MountPoint: "/data"}},
		}
		out := l.buildUrunitConfig()
		assert.Contains(t, out, "ID:FCdata\n")
	})

	t.Run("zero ProcessConfig still serializes UID/GID/WD lines", func(t *testing.T) {
		t.Parallel()
		l := &Linux{}
		out := l.buildUrunitConfig()
		assert.Contains(t, out, "UID:0\n")
		assert.Contains(t, out, "GID:0\n")
		assert.Contains(t, out, "WD:\n")
	})
}

func TestLinuxMonitorBlockCli(t *testing.T) {
	t.Parallel()

	t.Run("empty Blk slice returns nil", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Monitor: "qemu"}
		assert.Nil(t, l.MonitorBlockCli())
	})

	t.Run("qemu emits ExactArgs per block", func(t *testing.T) {
		t.Parallel()
		l := &Linux{
			Monitor: "qemu",
			Blk: []types.BlockDevParams{
				{ID: "data", Source: "/d"},
				{ID: "logs", Source: "/l"},
			},
		}
		args := l.MonitorBlockCli()
		require.Len(t, args, 2)
		assert.Contains(t, args[0].ExactArgs, "virtio-blk-pci,serial=data,drive=data")
		assert.Contains(t, args[0].ExactArgs, "id=data,file=/d")
		assert.Empty(t, args[0].ID)
		assert.Empty(t, args[0].Path)
		assert.Contains(t, args[1].ExactArgs, "serial=logs,drive=logs")
	})

	t.Run("firecracker fills ID/Path with FC prefix on ID", func(t *testing.T) {
		t.Parallel()
		l := &Linux{
			Monitor: "firecracker",
			Blk:     []types.BlockDevParams{{ID: "data", Source: "/d"}},
		}
		args := l.MonitorBlockCli()
		require.Len(t, args, 1)
		assert.Equal(t, "FCdata", args[0].ID)
		assert.Equal(t, "/d", args[0].Path)
		assert.Empty(t, args[0].ExactArgs)
	})

	t.Run("unknown monitor returns nil", func(t *testing.T) {
		t.Parallel()
		l := &Linux{
			Monitor: "cloud-hypervisor",
			Blk:     []types.BlockDevParams{{ID: "data", Source: "/d"}},
		}
		assert.Nil(t, l.MonitorBlockCli())
	})
}

func TestLinuxMonitorCli(t *testing.T) {
	t.Parallel()

	t.Run("qemu without urunit returns OtherArgs only", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Monitor: "qemu"}
		args := l.MonitorCli()
		assert.Equal(t, " -no-reboot -nodefaults", args.OtherArgs)
		assert.Empty(t, args.ExtraInitrd)
	})

	t.Run("qemu with urunit and initrd rootfs omits ExtraInitrd", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Monitor: "qemu", InitrdConf: true, RootFsType: "initrd"}
		args := l.MonitorCli()
		assert.Equal(t, " -no-reboot -nodefaults", args.OtherArgs)
		assert.Empty(t, args.ExtraInitrd)
	})

	t.Run("qemu with urunit and non-initrd rootfs sets ExtraInitrd", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Monitor: "qemu", InitrdConf: true, RootFsType: "block"}
		args := l.MonitorCli()
		assert.Equal(t, " -no-reboot -nodefaults", args.OtherArgs)
		assert.Equal(t, urunitConfPath, args.ExtraInitrd)
	})

	t.Run("firecracker with urunit and non-initrd rootfs sets ExtraInitrd", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Monitor: "firecracker", InitrdConf: true, RootFsType: "block"}
		args := l.MonitorCli()
		assert.Equal(t, urunitConfPath, args.ExtraInitrd)
		assert.Empty(t, args.OtherArgs)
	})

	t.Run("firecracker without urunit returns zero", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Monitor: "firecracker"}
		assert.Equal(t, types.MonitorCliArgs{}, l.MonitorCli())
	})

	t.Run("unknown monitor returns zero", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Monitor: "cloud-hypervisor"}
		assert.Equal(t, types.MonitorCliArgs{}, l.MonitorCli())
	})
}

func TestLinuxCommandString(t *testing.T) {
	t.Parallel()

	expectedConsole := func(monitor string) string {
		if runtime.GOARCH == "arm64" && monitor == "qemu" {
			return "console=ttyAMA0"
		}
		return "console=ttyS0"
	}

	t.Run("block rootfs sets root=/dev/vda and init clause", func(t *testing.T) {
		t.Parallel()
		l := &Linux{
			RootFsType: "block",
			Monitor:    "qemu",
			App:        "/init",
			Command:    "arg1",
			Net:        LinuxNet{Address: "10.0.0.5", Gateway: "10.0.0.1", Mask: "255.255.255.0"},
		}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.Contains(t, out, "panic=-1")
		assert.Contains(t, out, expectedConsole("qemu"))
		assert.Contains(t, out, "root=/dev/vda rw")
		assert.Contains(t, out, "init=/init -- arg1")
		assert.NotContains(t, out, "rdinit=")
	})

	t.Run("initrd rootfs uses rdinit prefix and ram0", func(t *testing.T) {
		t.Parallel()
		l := &Linux{RootFsType: "initrd", Monitor: "qemu", App: "/init"}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.Contains(t, out, "root=/dev/ram0 rw")
		assert.Contains(t, out, "rdinit=/init")
	})

	t.Run("9pfs rootfs sets virtio 9p mount params", func(t *testing.T) {
		t.Parallel()
		l := &Linux{RootFsType: "9pfs"}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.Contains(t, out, "rootfstype=9p")
		assert.Contains(t, out, "trans=virtio,version=9p2000.L")
	})

	t.Run("virtiofs rootfs sets virtiofs mount", func(t *testing.T) {
		t.Parallel()
		l := &Linux{RootFsType: "virtiofs"}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.Contains(t, out, "root=fs0 rw rootfstype=virtiofs")
	})

	t.Run("unknown rootfs type omits root= clause", func(t *testing.T) {
		t.Parallel()
		l := &Linux{RootFsType: "unknown"}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.NotContains(t, out, "root=")
	})

	t.Run("no network omits ip= clause", func(t *testing.T) {
		t.Parallel()
		l := &Linux{RootFsType: "block"}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.NotContains(t, out, "ip=")
	})

	t.Run("network present sets ip= clause with urunc tag", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Net: LinuxNet{Address: "10.0.0.5", Gateway: "10.0.0.1", Mask: "255.255.255.0"}}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.Contains(t, out, "ip=10.0.0.5::10.0.0.1:255.255.255.0:urunc:eth0:off")
	})

	t.Run("env vars appended when InitrdConf is false", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Env: []string{"FOO=bar", "BAZ=qux"}}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.Contains(t, out, "FOO=bar")
		assert.Contains(t, out, "BAZ=qux")
		assert.NotContains(t, out, "URUNIT_CONFIG")
	})

	t.Run("InitrdConf with initrd rootfs uses inline urunit conf path", func(t *testing.T) {
		t.Parallel()
		l := &Linux{InitrdConf: true, RootFsType: "initrd", Env: []string{"FOO=bar"}}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.Contains(t, out, "URUNIT_CONFIG="+urunitConfPath)
		assert.NotContains(t, out, "retain_initrd")
		assert.NotContains(t, out, "FOO=bar", "env vars must not leak into bootParams when InitrdConf is true")
	})

	t.Run("InitrdConf with non-initrd rootfs uses retain_initrd path", func(t *testing.T) {
		t.Parallel()
		l := &Linux{InitrdConf: true, RootFsType: "block"}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.Contains(t, out, "retain_initrd")
		assert.Contains(t, out, "URUNIT_CONFIG="+retainInitrdPath)
	})

	t.Run("ip outside gateway subnet adds URUNIT_DEFROUTE", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Net: LinuxNet{Address: "10.0.0.5", Gateway: "192.168.1.1", Mask: "255.255.255.0"}}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.Contains(t, out, "URUNIT_DEFROUTE=1")
	})

	t.Run("ip inside gateway subnet does not add URUNIT_DEFROUTE", func(t *testing.T) {
		t.Parallel()
		l := &Linux{Net: LinuxNet{Address: "10.0.0.5", Gateway: "10.0.0.1", Mask: "255.255.255.0"}}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.NotContains(t, out, "URUNIT_DEFROUTE")
	})

	t.Run("empty App omits init= clause entirely", func(t *testing.T) {
		t.Parallel()
		l := &Linux{}
		out, err := l.CommandString()
		require.NoError(t, err)
		assert.NotContains(t, out, "init=")
	})
}

func TestLinuxInit(t *testing.T) {
	t.Parallel()

	t.Run("empty cmdline propagates parseCmdLine error", func(t *testing.T) {
		t.Parallel()
		l := newLinux()
		err := l.Init(types.UnikernelParams{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no init")
	})

	t.Run("non-urunit init does not write urunit config", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		l := newLinux()
		err := l.Init(types.UnikernelParams{
			CmdLine: []string{"/bin/sh"},
			Monitor: "qemu",
			Net:     types.NetDevParams{IP: "10.0.0.5", Gateway: "10.0.0.1", Mask: "255.255.255.0"},
			Rootfs:  types.RootfsParams{Type: "block", MonRootfs: tmp},
		})
		require.NoError(t, err)
		assert.Equal(t, "/bin/sh", l.App)
		assert.Equal(t, "block", l.RootFsType)
		assert.Equal(t, "qemu", l.Monitor)
		assert.False(t, l.InitrdConf)
		_, statErr := os.Stat(filepath.Join(tmp, urunitConfPath))
		assert.True(t, os.IsNotExist(statErr), "urunit.conf must not be created for non-urunit apps")
	})

	t.Run("urunit init writes config under MonRootfs for non-initrd rootfs", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		l := newLinux()
		err := l.Init(types.UnikernelParams{
			CmdLine:  []string{"/usr/bin/urunit"},
			Monitor:  "qemu",
			EnvVars:  []string{"FOO=bar"},
			Rootfs:   types.RootfsParams{Type: "block", MonRootfs: tmp},
			ProcConf: types.ProcessConfig{UID: 0, GID: 0, WorkDir: "/"},
		})
		require.NoError(t, err)
		assert.True(t, l.InitrdConf)
		content, readErr := os.ReadFile(filepath.Join(tmp, urunitConfPath))
		require.NoError(t, readErr)
		assert.Contains(t, string(content), "FOO=bar")
		assert.Contains(t, string(content), "WD:/\n")
	})

	t.Run("urunit init wraps initrd write errors with context", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		l := newLinux()
		err := l.Init(types.UnikernelParams{
			CmdLine: []string{"/usr/bin/urunit"},
			Rootfs: types.RootfsParams{
				Type:      "initrd",
				MonRootfs: filepath.Join(tmp, "does-not-exist"),
				Path:      "initrd.cpio",
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to setup urunit config")
	})
}
