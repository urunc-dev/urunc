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

	expected := "URUNIT1\x00" +
		"UES\x00PATH=/bin\x00CERT=-----BEGIN CERTIFICATE-----\nUEE\n-----END CERTIFICATE-----\x00UEE_MODE=1\x00UEE\x00" +
		"UCS\x00UID:0\x00GID:0\x00WD:/\x00UCE\x00" +
		"UBS\x00ID:vol0\x00MP:/data\x00UBE\x00"
	assert.Equal(t, expected, l.buildUrunitConfig())
}

func TestLinuxBuildUrunitConfigEmpty(t *testing.T) {
	t.Parallel()

	l := &Linux{Monitor: "firecracker", ProcConfig: types.ProcessConfig{WorkDir: "/"}}

	expected := "URUNIT1\x00UES\x00UEE\x00UCS\x00UID:0\x00GID:0\x00WD:/\x00UCE\x00UBS\x00UBE\x00"
	assert.Equal(t, expected, l.buildUrunitConfig())
}
