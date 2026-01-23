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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestFreeBSDCommandString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		freebsd  *FreeBSD
		expected string
	}{
		{
			name: "urunit init, ufs rootfs, config drive right after the rootfs",
			freebsd: &FreeBSD{
				WithUrunit:  true,
				InitPath:    "/urunit",
				BlockFsType: freeBSDufs,
				Block:       []types.BlockDevParams{{ID: "rootfs"}},
			},
			expected: "vfs.root.mountfrom=ufs:/dev/vtbd0" +
				" init_path=/urunit URUNIT_CONFIG=/dev/vtbd1",
		},
		{
			name: "urunit init, ext2fs rootfs with one volume",
			freebsd: &FreeBSD{
				WithUrunit:  true,
				InitPath:    "/urunit",
				BlockFsType: freeBSDext2fs,
				Block:       []types.BlockDevParams{{ID: "rootfs"}, {ID: "vol0"}},
			},
			expected: "vfs.root.mountfrom=ext2fs:/dev/vtbd0" +
				" init_path=/urunit URUNIT_CONFIG=/dev/vtbd2",
		},
		{
			name: "urunit init, 9pfs rootfs, config drive as vtbd0 (no volumes)",
			freebsd: &FreeBSD{
				WithUrunit: true,
				InitPath:   "/urunit",
				RootFsType: "9pfs",
			},
			expected: "vfs.root.mountfrom=p9fs:fs0" +
				" init_path=/urunit URUNIT_CONFIG=/dev/vtbd0",
		},
		{
			name: "urunit init, 9pfs rootfs with one volume, config drive after it",
			freebsd: &FreeBSD{
				WithUrunit: true,
				InitPath:   "/urunit",
				RootFsType: "9pfs",
				Block:      []types.BlockDevParams{{ID: "vol0"}},
			},
			expected: "vfs.root.mountfrom=p9fs:fs0" +
				" init_path=/urunit URUNIT_CONFIG=/dev/vtbd1",
		},
		{
			name: "urunit init, gateway outside the subnet requests a default route",
			freebsd: &FreeBSD{
				WithUrunit:  true,
				InitPath:    "/urunit",
				BlockFsType: freeBSDufs,
				Block:       []types.BlockDevParams{{ID: "rootfs"}},
				Net:         FreeBSDNet{Address: "10.244.1.5", Gateway: "169.254.1.1", Mask: "255.255.255.0"},
			},
			expected: "vfs.root.mountfrom=ufs:/dev/vtbd0" +
				" init_path=/urunit URUNIT_CONFIG=/dev/vtbd1 URUNIT_DEFROUTE=1",
		},
		{
			name: "urunit init, gateway inside the subnet",
			freebsd: &FreeBSD{
				WithUrunit:  true,
				InitPath:    "/urunit",
				BlockFsType: freeBSDufs,
				Block:       []types.BlockDevParams{{ID: "rootfs"}},
				Net:         FreeBSDNet{Address: "172.17.0.2", Gateway: "172.17.0.1", Mask: "255.255.0.0"},
			},
			expected: "vfs.root.mountfrom=ufs:/dev/vtbd0" +
				" init_path=/urunit URUNIT_CONFIG=/dev/vtbd1",
		},
		{
			name: "container command as init (no urunit): no URUNIT_* variables",
			freebsd: &FreeBSD{
				WithUrunit:  false,
				InitPath:    "/sbin/init",
				BlockFsType: freeBSDufs,
				Block:       []types.BlockDevParams{{ID: "rootfs"}},
				// Net is set, but without urunit no URUNIT_DEFROUTE is emitted.
				Net: FreeBSDNet{Address: "10.244.1.5", Gateway: "169.254.1.1", Mask: "255.255.255.0"},
			},
			expected: "vfs.root.mountfrom=ufs:/dev/vtbd0 init_path=/sbin/init",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := tc.freebsd.CommandString()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFreeBSDMonitorBlockCli(t *testing.T) {
	t.Parallel()

	blocks := []types.BlockDevParams{
		{ID: "rootfs", Source: "/rootfs.ufs", MountPoint: "/"},
		{ID: "vol0", Source: "/dev/loop3", MountPoint: "/data"},
	}

	// urunit init: rootfs + volume + the urunit config drive.
	fc := &FreeBSD{Monitor: "firecracker", WithUrunit: true, Block: blocks}
	fcArgs := fc.MonitorBlockCli()
	require.Len(t, fcArgs, 3)
	assert.Equal(t, "FCrootfs", fcArgs[0].ID)
	assert.Equal(t, "/rootfs.ufs", fcArgs[0].Path)
	assert.Equal(t, "FCvol0", fcArgs[1].ID)
	assert.Equal(t, "FC_URUNIT_CONFIG", fcArgs[2].ID)
	assert.Equal(t, urunitConfPath, fcArgs[2].Path)

	// Plain init: no config drive, just rootfs + volume.
	fcPlain := &FreeBSD{Monitor: "firecracker", Block: blocks}
	plainArgs := fcPlain.MonitorBlockCli()
	require.Len(t, plainArgs, 2)
	assert.Equal(t, "FCvol0", plainArgs[1].ID)

	// urunit init without any block device still attaches the config drive as vtbd0.
	fcNoBlk := &FreeBSD{Monitor: "firecracker", WithUrunit: true, InitPath: "/urunit", BlockFsType: freeBSDufs}
	require.Len(t, fcNoBlk.MonitorBlockCli(), 1)
	cmd, err := fcNoBlk.CommandString()
	require.NoError(t, err)
	assert.Contains(t, cmd, "URUNIT_CONFIG=/dev/vtbd0")

	qemu := &FreeBSD{Monitor: "qemu", WithUrunit: true, Block: blocks}
	qemuArgs := qemu.MonitorBlockCli()
	require.Len(t, qemuArgs, 3)
	assert.Equal(t, []string{
		"-device", "virtio-blk-device,serial=rootfs,drive=rootfs",
		"-drive", "format=raw,if=none,id=rootfs,file=/rootfs.ufs",
	}, qemuArgs[0].ExactArgs)
	assert.Equal(t, []string{
		"-device", "virtio-blk-device,serial=urunit,drive=urunit",
		"-drive", "format=raw,if=none,id=urunit,file=/urunit.conf",
	}, qemuArgs[2].ExactArgs)

	other := &FreeBSD{Monitor: "cloud-hypervisor", WithUrunit: true, Block: blocks}
	assert.Nil(t, other.MonitorBlockCli())

	// A 9pfs rootfs serves the rootfs over 9p, but the urunit config is still
	// attached as a raw virtio-blk device after the volumes.
	qemu9p := &FreeBSD{
		Monitor:    "qemu",
		WithUrunit: true,
		RootFsType: "9pfs",
		Block:      []types.BlockDevParams{{ID: "vol0", Source: "/dev/loop3"}},
	}
	nineArgs := qemu9p.MonitorBlockCli()
	require.Len(t, nineArgs, 2, "the volume plus the urunit config drive")
	assert.Equal(t, []string{
		"-device", "virtio-blk-device,serial=vol0,drive=vol0",
		"-drive", "format=raw,if=none,id=vol0,file=/dev/loop3",
	}, nineArgs[0].ExactArgs)
	assert.Equal(t, []string{
		"-device", "virtio-blk-device,serial=urunit,drive=urunit",
		"-drive", "format=raw,if=none,id=urunit,file=/urunit.conf",
	}, nineArgs[1].ExactArgs)
}

func TestFreeBSDMonitorSharedfsCli(t *testing.T) {
	t.Parallel()

	// FreeBSD on QEMU (microvm) exposes the 9p share over virtio-mmio.
	qemu := &FreeBSD{Monitor: "qemu"}
	assert.Equal(t, []string{
		"-fsdev", "local,id=rootfs9p,security_model=none,multidevs=remap,path=/srv/share",
		"-device", "virtio-9p-device,fsdev=rootfs9p,mount_tag=fs0",
	}, qemu.MonitorSharedfsCli("9pfs", "/srv/share"))

	// No shared-fs CLI for a non-9pfs type, or for Firecracker.
	assert.Nil(t, qemu.MonitorSharedfsCli("virtiofs", "/srv/share"))
	assert.Nil(t, (&FreeBSD{Monitor: "firecracker"}).MonitorSharedfsCli("9pfs", "/srv/share"))
}

func TestFreeBSDMonitorCli(t *testing.T) {
	t.Parallel()

	qemu := &FreeBSD{Monitor: "qemu"}
	assert.Contains(t, qemu.MonitorCli().OtherArgs, "microvm,acpi=on,pic=on,pit=on,rtc=on")
	assert.Contains(t, qemu.MonitorNetCli("tap0_urunc", "aa:bb:cc:dd:ee:ff"),
		"virtio-net-device,netdev=net0,mac=aa:bb:cc:dd:ee:ff")

	fc := &FreeBSD{Monitor: "firecracker"}
	assert.Equal(t, types.MonitorCliArgs{}, fc.MonitorCli())
	assert.Nil(t, fc.MonitorNetCli("tap0_urunc", "aa:bb:cc:dd:ee:ff"))
}

func TestFreeBSDBuildUrunitConfig(t *testing.T) {
	t.Parallel()

	f := &FreeBSD{
		Monitor: "firecracker",
		Command: []string{"/rescue/sh", "-c", "echo hello world"},
		Env:     []string{"PATH=/bin:/sbin", "FOO=bar"},
		Net:     FreeBSDNet{Address: "172.17.0.2", Gateway: "172.17.0.1", Mask: "255.255.0.0"},
		Block: []types.BlockDevParams{
			{ID: "rootfs", MountPoint: "/"},
			{ID: "vol0", MountPoint: "/data"},
		},
		ProcConfig: types.ProcessConfig{UID: 1001, GID: 1001, WorkDir: "/tmp"},
	}
	conf := f.buildUrunitConfig()

	assert.Equal(t, 0, len(conf)%blockSectorSize, "config must be a whole number of sectors")
	expectedHead := "UES\nPATH=/bin:/sbin\nFOO=bar\nUEE\n" +
		"UCS\nUID:1001\nGID:1001\nWD:/tmp\nARC:3\nARV:/rescue/sh\nARV:-c\nARV:echo hello world\nUCE\n" +
		"UBS\nID:FCvol0\nMP:/data\nUBE\n" +
		"UNS\nIP:172.17.0.2\nGW:172.17.0.1\nMSK:255.255.0.0\nUNE\n" +
		"PAD\n"
	assert.True(t, strings.HasPrefix(conf, expectedHead), "unexpected config:\n%s", conf)
	assert.Equal(t, "", strings.Trim(conf[len(expectedHead):], "\n"), "padding must be newlines only")
}

func TestFreeBSDUsesUrunit(t *testing.T) {
	t.Parallel()

	assert.True(t, usesUrunit([]string{"/urunit", "/bin/sh"}))
	assert.True(t, usesUrunit([]string{"urunit"}))
	assert.False(t, usesUrunit([]string{"/sbin/init"}))
	assert.False(t, usesUrunit(nil))
}

// TestFreeBSDPlainInit covers an image whose own command is the init (urunit is
// not present): Init derives the fields, provisions no urunit configuration
// (setupUrunitConfig is not called), and CommandString emits no URUNIT_* vars.
func TestFreeBSDPlainInit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	f := newFreeBSD()
	err := f.Init(types.UnikernelParams{
		CmdLine: []string{"/sbin/init"},
		Monitor: "qemu",
		Net:     types.NetDevParams{IP: "172.17.0.2", Mask: "255.255.0.0", Gateway: "172.17.0.1"},
		Block:   []types.BlockDevParams{{ID: "rootfs", Source: "/rootfs.ufs", MountPoint: "/"}},
		Rootfs:  types.RootfsParams{Type: "block", Path: "/rootfs.ufs", MonRootfs: dir},
	})
	require.NoError(t, err)
	assert.False(t, f.WithUrunit)
	assert.Equal(t, "/sbin/init", f.InitPath)
	assert.Equal(t, "block", f.RootFsType)
	assert.Equal(t, freeBSDufs, f.BlockFsType)

	// No urunit configuration file is written.
	_, statErr := os.Stat(filepath.Join(dir, urunitConfPath))
	assert.True(t, os.IsNotExist(statErr), "no urunit config must be written without urunit")

	// No extra config drive: only the rootfs block is attached.
	require.Len(t, f.MonitorBlockCli(), 1)

	cmd, err := f.CommandString()
	require.NoError(t, err)
	assert.Equal(t, "vfs.root.mountfrom=ufs:/dev/vtbd0 init_path=/sbin/init", cmd)
	assert.NotContains(t, cmd, "URUNIT_CONFIG")
}

func TestFreeBSDInitRootfsType(t *testing.T) {
	t.Parallel()

	// Init must fail loudly for any rootfs that is not block or 9pfs.
	// CmdLine is a plain init, so no urunit config is written to disk.
	for _, rootfs := range []string{"initrd", "virtiofs", "raw", ""} {
		f := newFreeBSD()
		err := f.Init(types.UnikernelParams{
			CmdLine: []string{"/bin/sh"},
			Rootfs:  types.RootfsParams{Type: rootfs},
		})
		assert.Error(t, err, "rootfs type %q must be rejected", rootfs)
	}

	// block and 9pfs are accepted.
	for _, rootfs := range []string{"block", "9pfs"} {
		f := newFreeBSD()
		err := f.Init(types.UnikernelParams{
			CmdLine: []string{"/bin/sh"},
			Rootfs:  types.RootfsParams{Type: rootfs},
		})
		assert.NoError(t, err, "rootfs type %q must be accepted", rootfs)
	}
}
