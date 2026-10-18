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
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLinuxConsolePerMonitor pins the whole kernel command line of a Linux
// guest on each monitor. QEMU and Vz get the virtio console. The others keep
// the console they had before Vz was added.
func TestLinuxConsolePerMonitor(t *testing.T) {
	t.Parallel()

	const (
		head = "panic=-1 quiet "
		tail = " root=fs0 rw rootfstype=virtiofs" +
			" ip=10.0.0.2::10.0.0.1:255.255.255.0:urunc:eth0:off" +
			" retain_initrd URUNIT_CONFIG=/sys/firmware/initrd" +
			" init=/bin/sh -- -c true"
		virtioConsole = "console=hvc0 8250.nr_uarts=0"
	)

	cloudHypervisorConsole := "console=ttyS0"
	hviConsole := "console=ttyS0"
	if runtime.GOARCH == "arm64" {
		cloudHypervisorConsole = "console=hvc0"
		hviConsole = "console=ttyAMA0"
	}

	tests := []struct {
		monitor string
		console string
	}{
		{"qemu", virtioConsole},
		{"vz", virtioConsole},
		{"firecracker", "console=ttyS0"},
		{"cloud-hypervisor", cloudHypervisorConsole},
		{"hvi", hviConsole},
	}

	for _, tt := range tests {
		t.Run(tt.monitor, func(t *testing.T) {
			t.Parallel()

			l := &Linux{
				App:        "/bin/sh",
				Command:    "-c true",
				Monitor:    tt.monitor,
				RootFsType: "virtiofs",
				InitrdConf: true,
				Net:        LinuxNet{Address: "10.0.0.2", Gateway: "10.0.0.1", Mask: "255.255.255.0"},
			}
			cmdline, err := l.CommandString()
			require.NoError(t, err)
			assert.Equal(t, head+tt.console+tail, cmdline)
		})
	}
}
