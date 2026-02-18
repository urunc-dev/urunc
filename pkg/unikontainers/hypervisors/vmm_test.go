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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVMMFactoryQemuVhostFalse(t *testing.T) {
	t.Parallel()
	factory, exists := vmmFactories[QemuVmm]
	assert.True(t, exists, "qemu factory should exist")

	vmm := factory.createFunc(QemuBinary, "/usr/bin/qemu-system-x86_64", false)
	qemu, ok := vmm.(*Qemu)
	assert.True(t, ok, "factory should return *Qemu")
	assert.False(t, qemu.vhost, "vhost should be false when passed as false")
}

func TestVMMFactoryQemuVhostTrue(t *testing.T) {
	t.Parallel()
	factory, exists := vmmFactories[QemuVmm]
	assert.True(t, exists, "qemu factory should exist")

	vmm := factory.createFunc(QemuBinary, "/usr/bin/qemu-system-x86_64", true)
	qemu, ok := vmm.(*Qemu)
	assert.True(t, ok, "factory should return *Qemu")
	assert.True(t, qemu.vhost, "vhost should be true when passed as true")
}

func TestVMMFactoryNonQemuIgnoresVhost(t *testing.T) {
	t.Parallel()

	// SPT factory should ignore vhost parameter
	factory, exists := vmmFactories[SptVmm]
	assert.True(t, exists, "spt factory should exist")

	vmm := factory.createFunc(SptBinary, "/usr/bin/solo5-spt", true)
	_, ok := vmm.(*SPT)
	assert.True(t, ok, "factory should return *SPT")

	// HVT factory should ignore vhost parameter
	factory, exists = vmmFactories[HvtVmm]
	assert.True(t, exists, "hvt factory should exist")

	vmm = factory.createFunc(HvtBinary, "/usr/bin/solo5-hvt", true)
	_, ok = vmm.(*HVT)
	assert.True(t, ok, "factory should return *HVT")

	// Firecracker factory should ignore vhost parameter
	factory, exists = vmmFactories[FirecrackerVmm]
	assert.True(t, exists, "firecracker factory should exist")

	vmm = factory.createFunc(FirecrackerBinary, "/usr/bin/firecracker", true)
	_, ok = vmm.(*Firecracker)
	assert.True(t, ok, "factory should return *Firecracker")
}
