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

package hypervisors

import (
    "encoding/json"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

type fakeUK struct {
    extraInitrd string
    blocks      []types.MonitorBlockArgs
}

func (f *fakeUK) Init(types.UnikernelParams) error { return nil }
func (f *fakeUK) CommandString() (string, error) { return "", nil }
func (f *fakeUK) SupportsBlock() bool { return false }
func (f *fakeUK) SupportsFS(string) bool { return false }
func (f *fakeUK) MonitorNetCli(string, string) string { return "" }
func (f *fakeUK) MonitorBlockCli() []types.MonitorBlockArgs { return f.blocks }
func (f *fakeUK) MonitorCli() types.MonitorCliArgs { return types.MonitorCliArgs{ExtraInitrd: f.extraInitrd} }
func TestBuildConfig_InitrdAndDefaultMemory(t *testing.T) {
    fc := &Firecracker{binaryPath: "/usr/bin/firecracker", binary: "firecracker"}

    args := types.ExecArgs{
        InitrdPath:    "",
        UnikernelPath: "/vm/kernel",
        Command:       "-n",
        MemSizeB:      0,
        VCPUs:         2,
    }
    uk := &fakeUK{extraInitrd: "/opt/initrd.img"}

    jsonBytes, _, _, err := fc.buildConfigAndCmd(args, uk)
    assert.NoError(t, err)

    var cfg FirecrackerConfig
    err = json.Unmarshal(jsonBytes, &cfg)
    assert.NoError(t, err)

    // when InitrdPath is empty, MonitorCli ExtraInitrd should be used
    assert.Equal(t, "/opt/initrd.img", cfg.Source.InitrdPath)

    // DefaultMemory should be used when MemSizeB is zero
    assert.Equal(t, DefaultMemory, cfg.Machine.MemSizeMiB)
}

func TestBuildConfig_MemTooSmallDefaults(t *testing.T) {
    fc := &Firecracker{binaryPath: "/usr/bin/firecracker", binary: "firecracker"}

    args := types.ExecArgs{
        MemSizeB:      512, // less than 1 MiB -> bytesToMiB == 0
        UnikernelPath: "/vm/kernel",
        Command:       "-n",
        VCPUs:         1,
    }
    uk := &fakeUK{}

    jsonBytes, _, _, err := fc.buildConfigAndCmd(args, uk)
    assert.NoError(t, err)

    var cfg FirecrackerConfig
    err = json.Unmarshal(jsonBytes, &cfg)
    assert.NoError(t, err)

    assert.Equal(t, DefaultMemory, cfg.Machine.MemSizeMiB)
}

func TestBuildConfig_VSockAndNetwork(t *testing.T) {
    fc := &Firecracker{binaryPath: "/usr/bin/firecracker", binary: "firecracker"}

    args := types.ExecArgs{
        VAccelType:    "vsock",
        VSockDevPath:  "/var/run",
        VSockDevID:    7,
        UnikernelPath: "/vm/kernel",
        Command:       "-n",
        VCPUs:         1,
        Net: types.NetDevParams{
            TapDev: "tap0",
            MAC:    "02:00:00:00:00:01",
        },
    }
    uk := &fakeUK{}

    jsonBytes, _, _, err := fc.buildConfigAndCmd(args, uk)
    assert.NoError(t, err)

    var cfg FirecrackerConfig
    err = json.Unmarshal(jsonBytes, &cfg)
    assert.NoError(t, err)

    // vsock configured
    assert.Equal(t, 7, cfg.VSock.GuestCID)
    assert.Equal(t, "/var/run/vaccel.sock", cfg.VSock.UDSPath)
    assert.Equal(t, "root", cfg.VSock.VSockID)

    // net interface configured
    if assert.Len(t, cfg.NetIfs, 1) {
        assert.Equal(t, "tap0", cfg.NetIfs[0].HostIF)
        assert.Equal(t, "02:00:00:00:00:01", cfg.NetIfs[0].GuestMAC)
    }
}
