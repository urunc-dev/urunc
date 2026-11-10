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

func TestValidateUrunitValue(t *testing.T) {
	t.Parallel()

	// Newlines are data now, only NUL is rejected.
	assert.NoError(t, validateUrunitValue("env var", "FOO=bar"))
	assert.NoError(t, validateUrunitValue("env var", ""))
	assert.NoError(t, validateUrunitValue("env var", "EVIL=line1\nUEE"))
	assert.NoError(t, validateUrunitValue("env var", "EVIL=line1\r\nUEE"))
	assert.NoError(t, validateUrunitValue("working directory", "/tmp\n/injected"))
	assert.Error(t, validateUrunitValue("env var", "EVIL=line1\x00UEE"))
	assert.Error(t, validateUrunitValue("working directory", "/tmp\x00/evil"))
	assert.Error(t, validateUrunitValue("block mountpoint", "/data\x00UBE"))
	assert.Error(t, validateUrunitValue("command argument", "hello\x00world"))
}

func TestLinuxBuildUrunitConfigNULSeparated(t *testing.T) {
	t.Parallel()

	base := func() *Linux {
		return &Linux{
			Monitor: "qemu",
			Env:     []string{"PATH=/bin:/sbin", "FOO=bar"},
			Blk: []types.BlockDevParams{
				{ID: "rootfs", MountPoint: "/"},
				{ID: "vol0", MountPoint: "/data"},
			},
			ProcConfig: types.ProcessConfig{UID: 1000, GID: 1000, WorkDir: "/tmp"},
		}
	}

	// Regression: valid config builds with NUL-terminated records.
	conf, err := base().buildUrunitConfig()
	require.NoError(t, err)
	assert.Contains(t, conf, "UES\x00PATH=/bin:/sbin\x00FOO=bar\x00UEE\x00")
	assert.Contains(t, conf, "WD:/tmp\x00")

	// Issue #1068 scenario: newline in values stays data, config still builds.
	newlineEnv := base()
	newlineEnv.Env = []string{"PATH=/bin:/sbin", "EVIL=line1\nUEE\nFAKE=injected"}
	conf, err = newlineEnv.buildUrunitConfig()
	require.NoError(t, err)
	assert.Contains(t, conf, "EVIL=line1\nUEE\nFAKE=injected\x00")
	assert.Contains(t, conf, "\x00UEE\x00")

	newlineWorkDir := base()
	newlineWorkDir.ProcConfig.WorkDir = "/tmp\n/injected"
	_, err = newlineWorkDir.buildUrunitConfig()
	require.NoError(t, err)

	newlineMount := base()
	newlineMount.Blk = []types.BlockDevParams{
		{ID: "vol0", MountPoint: "/data\nUBE"},
	}
	_, err = newlineMount.buildUrunitConfig()
	require.NoError(t, err)

	// Only NUL is rejected.
	nulEnv := base()
	nulEnv.Env = []string{"FOO=bar\x00baz"}
	_, err = nulEnv.buildUrunitConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NUL")
}

func TestFreeBSDBuildUrunitConfigNULSeparated(t *testing.T) {
	t.Parallel()

	base := func() *FreeBSD {
		return &FreeBSD{
			Monitor: "firecracker",
			Command: []string{"/rescue/sh", "-c", "echo hello"},
			Env:     []string{"PATH=/bin:/sbin", "FOO=bar"},
			Net:     FreeBSDNet{Address: "172.17.0.2", Gateway: "172.17.0.1", Mask: "255.255.0.0"},
			Block: []types.BlockDevParams{
				{ID: "rootfs", MountPoint: "/"},
				{ID: "vol0", MountPoint: "/data"},
			},
			ProcConfig: types.ProcessConfig{UID: 1001, GID: 1001, WorkDir: "/tmp"},
		}
	}

	// Regression: valid config builds and stays sector-aligned.
	conf, err := base().buildUrunitConfig()
	require.NoError(t, err)
	assert.Equal(t, 0, len(conf)%blockSectorSize)
	assert.True(t, strings.HasPrefix(conf, "UES\x00PATH=/bin:/sbin\x00"))

	// Issue #1068 scenarios now survive as data.
	newlineEnv := base()
	newlineEnv.Env = []string{"EVIL=line1\nUEE\nFAKE=injected"}
	_, err = newlineEnv.buildUrunitConfig()
	require.NoError(t, err)

	newlineArg := base()
	newlineArg.Command = []string{"/rescue/sh", "hello\nARV:injected"}
	conf, err = newlineArg.buildUrunitConfig()
	require.NoError(t, err)
	assert.Contains(t, conf, "ARV:hello\nARV:injected\x00")

	// NUL is the only rejected byte.
	nulArg := base()
	nulArg.Command = []string{"/rescue/sh", "hello\x00world"}
	_, err = nulArg.buildUrunitConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command argument")

	nulMount := base()
	nulMount.Block = []types.BlockDevParams{
		{ID: "vol0", MountPoint: "/data\x00MP:/evil"},
	}
	_, err = nulMount.buildUrunitConfig()
	require.Error(t, err)
}
