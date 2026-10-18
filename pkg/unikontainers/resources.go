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

// Platform-neutral sizing of the guest: memory and vCPUs derived from the OCI
// resources and the monitor defaults, and the guest log verbosity. The Linux
// engine and the darwin prologue (buildExecContext) size the guest through the
// same helpers.

package unikontainers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

// monitorMemoryBytes returns the guest memory size in bytes, honoring a memory
// limit from the OCI spec and falling back to the monitor's configured default.
func monitorMemoryBytes(defaultMem uint, resources *specs.LinuxResources) uint64 {
	mem := uint64(defaultMem * 1024 * 1024)
	if resources != nil && resources.Memory != nil {
		if resources.Memory.Limit != nil && *resources.Memory.Limit > 0 {
			mem = uint64(*resources.Memory.Limit) // nolint:gosec
		}
	}

	return mem
}

// maxCpusetCPUs bounds how many CPUs a cpuset list may name before it is
// taken as malformed. It is far above any host, and it keeps a range such as
// "0-4294967295" from allocating a set that size.
const maxCpusetCPUs = 1 << 16

// monitorVCPUs returns the vCPU count for a container boot guest, taken from
// the container's CPU resources and capped at hostCPUs.
//
// A CPU quota gives ceil(quota/period) vCPUs, which is what `nerdctl run
// --cpus` sets. A cpuset gives the number of CPUs it names. When both are set
// the smaller wins. Without either, or with a cpuset that cannot be read, it
// returns defaultVCPUs.
func monitorVCPUs(defaultVCPUs uint, resources *specs.LinuxResources, hostCPUs uint) uint {
	n := defaultVCPUs
	if resources != nil && resources.CPU != nil {
		cpu := resources.CPU
		var fromQuota, fromSet uint
		if cpu.Quota != nil && *cpu.Quota > 0 && cpu.Period != nil && *cpu.Period > 0 {
			quota, period := uint64(*cpu.Quota), *cpu.Period // nolint:gosec
			fromQuota = uint((quota + period - 1) / period)
		}
		if cpu.Cpus != "" {
			count, err := cpusetCount(cpu.Cpus)
			if err != nil {
				uniklog.WithError(err).Warn("ignoring the container's cpuset for the vCPU count")
			}
			fromSet = count
		}
		switch {
		case fromQuota > 0 && fromSet > 0:
			n = min(fromQuota, fromSet)
		case fromQuota > 0:
			n = fromQuota
		case fromSet > 0:
			n = fromSet
		}
	}
	if hostCPUs > 0 && n > hostCPUs {
		uniklog.WithFields(logrus.Fields{
			"requested": n,
			"host CPUs": hostCPUs,
		}).Warn("capping the guest's vCPUs at the CPUs urunc may run on")
		n = hostCPUs
	}
	if n < 1 {
		n = 1
	}

	return n
}

// cpusetCount returns how many distinct CPUs a cpuset list names, for
// instance 5 for "0-3,6".
func cpusetCount(list string) (uint, error) {
	seen := map[uint64]struct{}{}
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		first, err := strconv.ParseUint(lo, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("cpuset %q: %w", list, err)
		}
		last := first
		if isRange {
			last, err = strconv.ParseUint(hi, 10, 32)
			if err != nil {
				return 0, fmt.Errorf("cpuset %q: %w", list, err)
			}
			if last < first {
				return 0, fmt.Errorf("cpuset %q: range %q runs backwards", list, part)
			}
		}
		if last-first >= maxCpusetCPUs || len(seen)+int(last-first) >= maxCpusetCPUs {
			return 0, fmt.Errorf("cpuset %q names more than %d CPUs", list, maxCpusetCPUs)
		}
		for c := first; c <= last; c++ {
			seen[c] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return 0, fmt.Errorf("cpuset %q names no CPU", list)
	}

	return uint(len(seen)), nil
}

// verboseGuestLogs reports whether the guest should log fully, mirroring
// urunc's own effective log verbosity. Every urunc process sets the global
// logger level from the live config (and the --debug flag) at startup, so it is
// read from there rather than from u.UruncCfg, which is rebuilt from state.json
// and deliberately does not carry the log level. A debug (or trace) level is
// verbose; anything quieter keeps the guest quiet.
func verboseGuestLogs() bool {
	return logrus.GetLevel() >= logrus.DebugLevel
}
