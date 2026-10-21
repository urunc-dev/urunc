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
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestLinuxBuildUrunitConfig(t *testing.T) {
	t.Parallel()

	l := &Linux{
		Monitor: "qemu",
		// Values that would break a new line separated configuration.
		Env: []string{
			"PATH=/bin",
			"CERT=-----BEGIN CERTIFICATE-----\nUEE\n-----END CERTIFICATE-----",
			"UEE_MODE=1",
		},
		Blk: []types.BlockDevParams{
			{ID: "rootfs", MountPoint: "/"},
			{ID: "vol0", MountPoint: "/data"},
		},
		ProcConfig: types.ProcessConfig{UID: 0, GID: 0, WorkDir: "/"},
	}

	expected := "UES\x00PATH=/bin\x00CERT=-----BEGIN CERTIFICATE-----\nUEE\n-----END CERTIFICATE-----\x00UEE_MODE=1\x00UEE\x00" +
		"UCS\x00UID:0\x00GID:0\x00WD:/\x00UCE\x00" +
		"UBS\x00ID:vol0\x00MP:/data\x00UBE\x00"
	assert.Equal(t, expected, l.buildUrunitConfig())
}

func TestLinuxBuildUrunitConfigEmpty(t *testing.T) {
	t.Parallel()

	l := &Linux{Monitor: "firecracker", ProcConfig: types.ProcessConfig{WorkDir: "/"}}

	expected := "UES\x00UEE\x00UCS\x00UID:0\x00GID:0\x00WD:/\x00UCE\x00UBS\x00UBE\x00"
	assert.Equal(t, expected, l.buildUrunitConfig())
}

// urunit finds a block volume by the serial its ID names, so hvi and Cloud
// Hypervisor get the ID as the serial. The rootfs disk is /dev/vda and gets
// none, which keeps hvi's plain --disk form for it.
func TestLinuxMonitorBlockCliSerials(t *testing.T) {
	t.Parallel()

	blk := []types.BlockDevParams{
		{ID: "rootfs", Source: "/dev/dm-3"},
		{ID: "data", Source: "/dev/dm-4"},
		{ID: "disk0", Source: "/disks/lower.ext4", ReadOnly: true},
	}
	for _, monitor := range []string{"hvi", "cloud-hypervisor"} {
		l := &Linux{Monitor: monitor, Blk: blk}
		assert.Equal(t, []types.MonitorBlockArgs{
			{ID: "rootfs", Path: "/dev/dm-3"},
			{ID: "data", Path: "/dev/dm-4", Serial: "data"},
			{ID: "disk0", Path: "/disks/lower.ext4", Serial: "disk0", ReadOnly: true},
		}, l.MonitorBlockCli(), monitor)
	}

	l := &Linux{Monitor: "qemu", Blk: blk[2:]}
	assert.Equal(t, []types.MonitorBlockArgs{{ExactArgs: []string{
		"-device", "virtio-blk-pci,serial=disk0,drive=disk0",
		"-drive", "format=raw,if=none,id=disk0,file=/disks/lower.ext4,readonly=on",
	}}}, l.MonitorBlockCli())
}

// An overlay-block root is named by the initrd files, so the command line
// has no root=, which /init would refuse next to them.
func TestLinuxCommandStringOverlayBlock(t *testing.T) {
	t.Parallel()

	l := &Linux{
		App:           containerBootInit,
		Command:       "/bin/sh",
		Monitor:       "hvi",
		RootFsType:    "overlay-block",
		RootFsFsType:  "ext4",
		InitrdConf:    true,
		ContainerBoot: true,
	}
	cmd, err := l.CommandString()
	assert.NoError(t, err)
	assert.NotContains(t, cmd, "root=")
	assert.NotContains(t, cmd, "rootfstype=")
	assert.Contains(t, cmd, "rdinit=/init -- /bin/sh")
}

// urunit mounts the block volumes the configuration lists. The disks of an
// overlay-block root have no mount point and are the guest init's.
func TestLinuxBuildUrunitConfigSkipsLayerDisks(t *testing.T) {
	t.Parallel()

	conf := BuildUrunitConfig(nil, types.ProcessConfig{}, []types.BlockDevParams{
		{ID: "disk0", Source: "/urunc-disks/rootfs-lower.ext4", ReadOnly: true},
		{ID: "disk1", Source: "/urunc-disks/rootfs-upper.ext4"},
		{ID: "data", Source: "/dev/dm-4", MountPoint: "/data"},
	}, "hvi")
	assert.NotContains(t, conf, "disk0")
	assert.NotContains(t, conf, "disk1")
	assert.Contains(t, conf, "ID:data\x00MP:/data\x00")
}

func TestInitrdFileNameOK(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"/urunc-rootfs-lower", "/urunc-mounts", "/urunc-resolv.conf"} {
		assert.True(t, initrdFileNameOK(name), name)
	}
	for _, name := range []string{"/init", "/urunit", "/urunit.conf", "/urunc-", "urunc-mounts",
		"/urunc-x/../init", "/run/urunc-x", "/urunc-a\x00b"} {
		assert.False(t, initrdFileNameOK(name), "%q", name)
	}
}
