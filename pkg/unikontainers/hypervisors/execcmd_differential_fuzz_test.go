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
	"strconv"
	"strings"
	"testing"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// Cross-backend differential for BuildExecCmd: every backend must request the
// guest memory the OCI limit allows.

// diffExecCmdVMMs are the backends whose BuildExecCmd builds an argv; firecracker writes a file per call.
func diffExecCmdVMMs() map[string]types.VMM {
	return map[string]types.VMM{
		"hvt":              &HVT{binary: HvtBinary, binaryPath: "/usr/bin/" + HvtBinary},
		"spt":              &SPT{binary: SptBinary, binaryPath: "/usr/bin/" + SptBinary},
		"qemu":             &Qemu{binary: QemuBinary, binaryPath: "/usr/bin/" + QemuBinary},
		"cloud-hypervisor": &CloudHypervisor{binary: CloudHypervisorBinary, binaryPath: "/usr/bin/" + CloudHypervisorBinary},
		"hyperlight":       &Hyperlight{binary: HyperlightBinary, binaryPath: "/usr/bin/" + HyperlightBinary},
	}
}

// mib mirrors the production encoders' unit: qemu/cloud-hypervisor/spt/hvt
// all interpret their memory argument as binary MiB.
const mib = uint64(1024 * 1024)

// extractMemBytes returns the guest memory, in bytes, that a backend's argv
// actually requests -- or ok=false if this helper does not model that backend.
func extractMemBytes(vmm string, argv []string) (uint64, bool) {
	atoiMiB := func(s string) (uint64, bool) {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return 0, false
		}
		return n * mib, true
	}

	switch vmm {
	case "qemu":
		// "-m", "<N>M"
		for i := 0; i+1 < len(argv); i++ {
			if argv[i] == "-m" {
				return atoiMiB(strings.TrimSuffix(argv[i+1], "M"))
			}
		}
	case "cloud-hypervisor":
		// "--memory", "size=<N>M[,shared=on]"
		for i := 0; i+1 < len(argv); i++ {
			if argv[i] == "--memory" {
				v := argv[i+1]
				v = strings.TrimPrefix(v, "size=")
				if c := strings.IndexByte(v, ','); c >= 0 {
					v = v[:c]
				}
				return atoiMiB(strings.TrimSuffix(v, "M"))
			}
		}
	case "spt", "hvt":
		// a single "--mem=<N>" element
		for _, a := range argv {
			if strings.HasPrefix(a, "--mem=") {
				return atoiMiB(strings.TrimPrefix(a, "--mem="))
			}
		}
	}
	// hyperlight isn't modelled: raw-byte units, unverified convention, no
	// source to confirm bytes vs MiB. firecracker is excluded from
	// diffExecCmdVMMs entirely: it writes a JSON file per call.
	return 0, false
}

// FuzzBuildExecCmdMemoryAgreement: guest memory each backend requests must
// round-trip to the OCI byte limit.
func FuzzBuildExecCmdMemoryAgreement(f *testing.F) {
	f.Add(uint64(256*1024*1024), uint64(1))
	f.Add(uint64(1024*1024*1024), uint64(2))
	f.Add(uint64(512*1000*1000), uint64(1))

	f.Fuzz(func(t *testing.T, memSizeB, vcpus uint64) {
		if !strictOn("memory-limit") {
			return
		}
		// Stay in the realistic range so the oracle tests the conversion,
		// not BytesToStringMB's sub-1-unit fallback.
		if memSizeB < mib || memSizeB > (1<<42) {
			return
		}
		if vcpus == 0 || vcpus > 256 {
			return
		}

		params := types.UnikernelParams{CmdLine: []string{"/init"}}
		execArgs := types.ExecArgs{
			MemSizeB:      memSizeB,
			VCPUs:         uint(vcpus),
			UnikernelPath: "/unikernel/app",
			Command:       "console=ttyS0",
		}

		u, err := unikernels.New(unikernels.LinuxUnikernel)
		if err != nil {
			return
		}
		if err := u.Init(params); err != nil {
			return
		}

		wantMiB := memSizeB / mib

		for vmmName, vmm := range diffExecCmdVMMs() {
			argv, err := vmm.BuildExecCmd(execArgs, u)
			if err != nil {
				continue
			}
			gotBytes, ok := extractMemBytes(vmmName, argv)
			if !ok {
				continue // backend not modelled (see extractMemBytes)
			}

			if gotBytes != wantMiB*mib {
				t.Fatalf("%s requests %d bytes of guest memory for an OCI limit of %d bytes (expected %d, i.e. %d MiB). The container's cgroup is set from the OCI limit, so a guest that uses what it was told it has can be OOM-killed by the host.\n  argv: %q",
					vmmName, gotBytes, memSizeB, wantMiB*mib, wantMiB, argv)
			}
		}
	})
}
