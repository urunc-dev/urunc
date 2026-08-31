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

func TestUnikraftInitVersion(t *testing.T) {
	tests := []struct {
		name          string
		version       string
		rootfsType    string
		expectedErr   error
		wantNetAddr   string
		wantRootFS    string
		wantEnvCleared bool
	}{
		{
			name:        "Empty version returns ErrUndefinedVersion and sets current args",
			version:     "",
			rootfsType:  "initrd",
			expectedErr: ErrUndefinedVersion,
			wantNetAddr: "netdev.ip=10.0.0.2/24:10.0.0.1:8.8.8.8",
			wantRootFS:  "vfs.fstab=[ \"initrd0:/:extract:::\" ]",
		},
		{
			name:        "Invalid version returns ErrVersionParsing and sets current args",
			version:     "not-a-version",
			rootfsType:  "9pfs",
			expectedErr: ErrVersionParsing,
			wantNetAddr: "netdev.ip=10.0.0.2/24:10.0.0.1:8.8.8.8",
			wantRootFS:  "vfs.fstab=[ \"fs0:/:9pfs:::\" ]",
		},
		{
			name:        "Modern version (>= 0.16.1) sets current args and preserves env",
			version:     "0.16.1",
			rootfsType:  "initrd",
			expectedErr: nil,
			wantNetAddr: "netdev.ip=10.0.0.2/24:10.0.0.1:8.8.8.8",
			wantRootFS:  "vfs.fstab=[ \"initrd0:/:extract:::\" ]",
		},
		{
			name:        "Future version (> 0.16.1) sets current args",
			version:     "0.17.0",
			rootfsType:  "9pfs",
			expectedErr: nil,
			wantNetAddr: "netdev.ip=10.0.0.2/24:10.0.0.1:8.8.8.8",
			wantRootFS:  "vfs.fstab=[ \"fs0:/:9pfs:::\" ]",
		},
		{
			name:           "Legacy version (< 0.16.1) sets compat args and clears env",
			version:        "0.15.0",
			rootfsType:     "initrd",
			expectedErr:    nil,
			wantNetAddr:    "netdev.ipv4_addr=10.0.0.2",
			wantRootFS:     "vfs.rootfs=initrd",
			wantEnvCleared: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := newUnikraft()
			params := types.UnikernelParams{
				Version: tt.version,
				EnvVars: []string{"FOO=BAR"},
				CmdLine: []string{"/app"},
				Net: types.NetDevParams{
					IP:      "10.0.0.2",
					Gateway: "10.0.0.1",
					Mask:    "255.255.255.0",
				},
				Rootfs: types.RootfsParams{
					Type: tt.rootfsType,
				},
			}

			err := u.Init(params)
			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.wantNetAddr, u.Net.Address)
			assert.Equal(t, tt.wantRootFS, u.VFS.RootFS)

			if tt.wantEnvCleared {
				assert.Nil(t, u.Env)
			} else {
				assert.Equal(t, []string{"FOO=BAR"}, u.Env)
			}
		})
	}
}

func TestUnikraftCommandString(t *testing.T) {
	u := newUnikraft()
	params := types.UnikernelParams{
		Version: "0.16.1",
		EnvVars: []string{"ENV1=VAL1", "ENV2=VAL2"},
		CmdLine: []string{"/server", "--port", "8080"},
		Net: types.NetDevParams{
			IP:      "10.0.0.5",
			Gateway: "10.0.0.1",
			Mask:    "255.255.255.0",
		},
		Rootfs: types.RootfsParams{
			Type: "9pfs",
		},
	}

	err := u.Init(params)
	assert.NoError(t, err)

	cmdStr, err := u.CommandString()
	assert.NoError(t, err)
	assert.Contains(t, cmdStr, "Unikraft")
	assert.Contains(t, cmdStr, "env.vars=[ ENV1=VAL1 ENV2=VAL2 ]")
	assert.Contains(t, cmdStr, "netdev.ip=10.0.0.5/24:10.0.0.1:8.8.8.8")
	assert.Contains(t, cmdStr, "vfs.fstab=[ \"fs0:/:9pfs:::\" ]")
	assert.Contains(t, cmdStr, "-- /server --port 8080")
}

func TestUnikraftSupports(t *testing.T) {
	u := newUnikraft()
	assert.False(t, u.SupportsBlock())
	assert.True(t, u.SupportsFS("9pfs"))
	assert.False(t, u.SupportsFS("ext4"))
	assert.Empty(t, u.MonitorNetCli("tap0", "aa:bb:cc:dd:ee:ff"))
	assert.Nil(t, u.MonitorBlockCli())
	assert.Equal(t, types.MonitorCliArgs{}, u.MonitorCli())
}
