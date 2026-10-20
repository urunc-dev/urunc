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
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// More strict checks. They register themselves, so the suite builds without this file.
func init() {
	registerStrict(
		strictCheck{"block-attached", blockAttached},
		strictCheck{"initrd-mounts-merged", initrdMountsMerged},
		strictCheck{"cgroup-limits-kept", cgroupLimitsKept},
		strictCheck{"cpu-limit", cpuLimit},
		strictCheck{"mirage-args", mirageArgs},
		strictCheck{"hermit-args", hermitArgs},
		strictCheck{"queue-proxy-idempotent", queueProxyIdempotent},
		strictCheck{"cmdline-passed", cmdlinePassed},
	)
	hookChecks = append(hookChecks, hookArgv)
}

// blockAttached: every block device urunc accepted is given to the monitor.
func blockAttached(r *fuzzRun) []string {
	if !r.done() || !slices.Contains(argvMonitors, hypervisors.VmmType(r.ms.MonitorType)) {
		return nil
	}
	cmd := strings.Join(r.argv, " ")
	for _, b := range r.ms.GuestParams.Block {
		if b.Source != "" && !strings.Contains(cmd, b.Source) {
			return []string{r.ms.MonitorType + "-" + r.ms.UnikernelType}
		}
	}
	return nil
}

// initrdMountsMerged: a file bind-mounted into a guest with an initrd rootfs is in that initrd at
// its destination, in an archive the kernel can unpack.
func initrdMountsMerged(r *fuzzRun) []string {
	if !r.done() || r.rp.Type != "initrd" || r.host>>6 == 2 {
		return nil
	}
	data, err := os.ReadFile(r.initrdHost)
	if err != nil {
		return nil
	}
	return initrdProblems(data, fileMountDests(r.u.Spec.Mounts))
}

// cgroupLimitsKept: the monitor's libcontainer config keeps the container's cgroup memory and CPU limits.
func cgroupLimitsKept(r *fuzzRun) []string {
	if r.cc == nil || r.cc.Cgroups == nil || r.u.Spec.Linux == nil || r.u.Spec.Linux.Resources == nil {
		return nil
	}
	res, got := r.u.Spec.Linux.Resources, r.cc.Cgroups.Resources
	var v []string
	if res.Memory != nil && res.Memory.Limit != nil && *res.Memory.Limit > 0 && (got == nil || got.Memory != *res.Memory.Limit) {
		v = append(v, "memory-limit-dropped")
	}
	if res.CPU != nil && res.CPU.Quota != nil && *res.CPU.Quota > 0 && (got == nil || got.CpuQuota != *res.CPU.Quota) {
		v = append(v, "cpu-quota-dropped")
	}
	return v
}

// cpuLimit: a container with a CPU quota gets that many vCPUs, rounded up, as it does for memory.
func cpuLimit(r *fuzzRun) []string {
	res := r.u.Spec.Linux
	if !r.done() || res == nil || res.Resources == nil || res.Resources.CPU == nil {
		return nil
	}
	c := res.Resources.CPU
	if c.Quota == nil || c.Period == nil || *c.Quota <= 0 || *c.Period == 0 {
		return nil
	}
	if want := (uint64(*c.Quota) + *c.Period - 1) / *c.Period; want != uint64(r.ms.ExecArgs.VCPUs) {
		return []string{"quota-ignored"}
	}
	return nil
}

// mirageArgs: a Mirage guest gets each arg intact.
func mirageArgs(r *fuzzRun) []string {
	if _, ok := r.guest.(*unikernels.Mirage); !r.done() || !ok || !r.engineShaped() {
		return nil
	}
	for _, a := range r.ms.GuestParams.CmdLine {
		if strings.ContainsAny(a, " \"\\") {
			return []string{"arg-with-space-quote-or-backslash"}
		}
	}
	return nil
}

// hermitArgs: a Hermit guest gets the container's args.
func hermitArgs(r *fuzzRun) []string {
	if _, ok := r.guest.(*unikernels.Hermit); !r.done() || !ok || !r.engineShaped() {
		return nil
	}
	words, ok := hermitShellWords(r.command)
	if !ok {
		return []string{"cmdline-does-not-shell-split-kernel-panics"}
	}
	var app []string
	if i := slices.Index(words, "--"); i >= 0 {
		app = words[i+1:]
	}
	if !slices.Equal(app, r.ms.GuestParams.CmdLine) {
		return []string{"args-differ"}
	}
	return nil
}

// queueProxyIdempotent: creating a queue-proxy container twice leaves the same config.json.
func queueProxyIdempotent(r *fuzzRun) []string {
	if r.rewritten == nil {
		return nil
	}
	var v []string
	if !reflect.DeepEqual(r.secondErr, ErrQueueProxy) {
		v = append(v, "second-create-fails-differently")
	}
	if string(r.rewritten) != string(r.rewrittenAgain) {
		v = append(v, "config-changes-again")
	}
	return v
}

// hookArgv: a hook runs with the container's hook args[0] as its argv[0].
func hookArgv(t *testing.T, h hookRun) {
	if !strictOn("hooks-argv") {
		return
	}
	for i := range h.n {
		got, _ := os.ReadFile(filepath.Join(h.out, "argv0."+strconv.Itoa(i)))
		if want := "hook-" + strconv.Itoa(i); strings.TrimSpace(string(got)) != want {
			t.Fatalf("%s: hook %d runs with argv[0] %q, the spec's args[0] is %q", h.name, i, got, want)
		}
	}
}

// cmdlinePassed: the monitor is given the guest command line.
func cmdlinePassed(r *fuzzRun) []string {
	vmm := hypervisors.VmmType(r.ms.MonitorType)
	if !r.done() || !(slices.Contains(argvMonitors, vmm) || vmm == hypervisors.HyperlightVmm) || r.command == "" {
		return nil
	}
	if !slices.Contains(r.argv, r.command) {
		return []string{string(vmm) + "-" + r.ms.UnikernelType}
	}
	return nil
}
