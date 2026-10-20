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

package unikontainers

import (
	"io"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

// discardLogs silences logrus for the run: per-iteration logging caps
// throughput and writes gigabytes of noise over a long campaign.
func discardLogs() {
	logrus.SetOutput(io.Discard)
	logrus.SetLevel(logrus.PanicLevel)
}

// FuzzUnikernelConfigSpecRoundTrip: same inverse property on the OCI
// annotation side (Map / getConfigFromSpec) -- a lossy field means the
// unikernel launches with a different config than the image asked for.
func FuzzUnikernelConfigSpecRoundTrip(f *testing.F) {
	discardLogs()

	// UnikernelCmd was removed from UnikernelConfig upstream, so the struct and
	// this target now carry ten fields rather than eleven.
	f.Add("qemu", "1.0", "/unikernel/app", "hvt", "/initrd", "/.boot/disk", "/data", "true", "tap0", "blk0")
	f.Add("", "", "", "", "", "", "", "", "", "")
	f.Add("rumprun", "", "bin", "spt", "", "", "", "false", "", "")

	f.Fuzz(func(t *testing.T, ukType, ukVer, bin, hv, initrd, block, blkMnt, mountRootfs, netDev, blkDev string) {
		orig := UnikernelConfig{
			UnikernelType:    ukType,
			UnikernelVersion: ukVer,
			UnikernelBinary:  bin,
			Hypervisor:       hv,
			Initrd:           initrd,
			Block:            block,
			BlkMntPoint:      blkMnt,
			MountRootfs:      mountRootfs,
			NetDev:           netDev,
			BlkDev:           blkDev,
		}

		spec := &specs.Spec{Annotations: orig.Map()}
		got := getConfigFromSpec(spec)
		if got == nil {
			t.Fatalf("getConfigFromSpec returned nil for annotations %#v", spec.Annotations)
		}
		if *got != orig {
			t.Fatalf("UnikernelConfig did not survive the Map()/getConfigFromSpec "+
				"round trip through the OCI annotations:\n  wrote: %#v\n  read:  %#v\n  annotations: %#v",
				orig, *got, spec.Annotations)
		}
	})
}
