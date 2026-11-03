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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinuxCommandStringConsole(t *testing.T) {
	t.Parallel()

	for _, monitor := range []string{"qemu", "firecracker", "cloud-hypervisor"} {
		for _, rootfs := range []string{"initrd", "block", "virtiofs"} {
			t.Run(monitor+"/"+rootfs, func(t *testing.T) {
				t.Parallel()

				console := "console=ttyS0"
				if runtime.GOARCH == "arm64" {
					switch monitor {
					case "qemu":
						console = "console=ttyAMA0"
					case "cloud-hypervisor":
						console = "console=hvc0"
					}
				}
				linux := &Linux{
					Monitor:    monitor,
					RootFsType: rootfs,
					App:        "/urunit",
					Command:    "/bin/echo 'hello world' --flag",
					InitrdConf: true,
				}
				cmdline, err := linux.CommandString()
				require.NoError(t, err)
				kernelArgs, appArgs, found := strings.Cut(cmdline, " -- ")
				require.True(t, found)
				assert.Contains(t, strings.Fields(kernelArgs), console)
				assert.Equal(t, linux.Command, appArgs)
				if rootfs == "initrd" {
					assert.Contains(t, kernelArgs, "rdinit=/urunit")
					assert.Contains(t, kernelArgs, "URUNIT_CONFIG=/urunit.conf")
				} else {
					assert.Contains(t, kernelArgs, " init=/urunit")
					assert.Contains(t, kernelArgs, "retain_initrd URUNIT_CONFIG=/sys/firmware/initrd")
				}
			})
		}
	}
}
