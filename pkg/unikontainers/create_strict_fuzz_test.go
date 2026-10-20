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
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/opencontainers/runc/libcontainer/configs"
	specs "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// strictSwitch is read from URUNC_FUZZ_STRICT: "1" or "all" switch on every strict check, a
// list of names (memory-limit,linux-cmdline) only those. The strict checks fail on main today,
// so they are off, and get turned on one by one as what they find gets fixed.
var strictSwitch = func() map[string]bool {
	set := map[string]bool{}
	for _, name := range strings.Split(os.Getenv("URUNC_FUZZ_STRICT"), ",") {
		if name = strings.TrimSpace(name); name == "1" {
			name = "all"
		}
		if name != "" {
			set[name] = true
		}
	}
	return set
}()

func strictOn(name string) bool { return strictSwitch["all"] || strictSwitch[name] }

// fuzzRun is what one input of FuzzCreate left behind, for the strict checks.
type fuzzRun struct {
	stage          string // the last stage reached; "argv" when the monitor's command was built
	command        string
	argv           []string
	host           byte
	bundle         string
	u              *Unikontainer // what New created, nil when it refused
	guest          types.Unikernel
	ms             monitorSpec // the monitor process's spec, as it has it just before the monitor runs
	rp             types.RootfsParams
	cc             *configs.Config // the monitor's libcontainer config, nil without cgo
	fcConfig       []byte          // Firecracker's config
	initrdHost     string          // where the image's initrd is on the host
	rewritten      []byte          // config.json after a queue-proxy create, and after a second one
	rewrittenAgain []byte
	secondErr      error
}

func (r *fuzzRun) done() bool { return r.stage == "argv" }

// engineShaped reports whether the container's args and env look like what an engine hands
// over: a command that is not blank or padded, env entries that are NAME=value, and strings
// that are valid UTF-8 without a NUL, as the C strings of exec and JSON are.
func (r *fuzzRun) engineShaped() bool {
	p := r.u.Spec.Process
	if p == nil || len(p.Args) == 0 || strings.TrimSpace(p.Args[0]) == "" || strings.TrimSpace(p.Args[0]) != p.Args[0] {
		return false
	}
	for _, e := range p.Env {
		if name, _, ok := strings.Cut(e, "="); !ok || name == "" || strings.TrimSpace(name) != name {
			return false
		}
	}
	for _, s := range append(append([]string{p.Cwd}, p.Args...), p.Env...) {
		if strings.ContainsRune(s, 0) || strings.ContainsRune(s, '�') {
			return false
		}
	}
	return true
}

// strictCheck judges a finished run. It returns the classes of violation, none when the
// check holds or does not apply.
type strictCheck struct {
	name  string
	check func(r *fuzzRun) []string
}

var strictChecks []strictCheck

func registerStrict(checks ...strictCheck) { strictChecks = append(strictChecks, checks...) }

// runStrict fails the input if a strict check that is on finds a violation.
func runStrict(t *testing.T, r *fuzzRun) {
	for _, c := range strictChecks {
		if !strictOn(c.name) {
			continue
		}
		if v := c.check(r); len(v) > 0 {
			t.Fatalf("strict check %s: %s", c.name, strings.Join(v, "; "))
		}
	}
}

// checkSpecComplete fails if urunc accepted a container without a root or a process
// section; with the check off, writeFuzzBundle keeps such containers out.
func checkSpecComplete(t *testing.T, spec *specs.Spec) {
	if strictOn("spec-complete") && (spec.Root == nil || spec.Process == nil) {
		t.Fatalf("New accepted a container without a root or a process section: %+v", spec)
	}
}

func init() {
	registerStrict(
		strictCheck{"memory-limit", memoryLimit},
		strictCheck{"queue-proxy-env", queueProxyEnv},
		strictCheck{"firecracker-config", firecrackerConfig},
		strictCheck{"unikraft-args-env", unikraftArgsEnv},
		strictCheck{"rumprun-args-env", rumprunArgsEnv},
		strictCheck{"linux-cmdline", linuxCmdline},
		strictCheck{"initrd-no-conflicts", initrdNoConflicts},
	)
}

// memoryLimit: the monitor is not given more memory than urunc worked out from the container's limit.
func memoryLimit(r *fuzzRun) []string {
	vmm, limit := hypervisors.VmmType(r.ms.MonitorType), r.ms.ExecArgs.MemSizeB
	if mem, ok := monitorMemMiB(vmm, r.argv); r.done() && ok && limit >= 1<<20 && mem<<20 > limit {
		return []string{string(vmm)}
	}
	return nil
}

// queueProxyEnv: the config.json urunc rewrites for a queue-proxy container has well-formed
// env entries and REDIRECT_IP exactly once.
func queueProxyEnv(r *fuzzRun) []string {
	var spec specs.Spec
	if r.rewritten == nil || json.Unmarshal(r.rewritten, &spec) != nil || spec.Process == nil {
		return nil
	}
	var v []string
	redirects := 0
	for _, env := range spec.Process.Env {
		if env == "" {
			v = append(v, "empty-env-entry")
		}
		if strings.HasPrefix(env, "REDIRECT_IP=") {
			redirects++
		}
	}
	if redirects != 1 {
		v = append(v, "redirect-ip-not-once")
	}
	return v
}

// firecrackerConfig: what was configured is in the config file urunc builds for Firecracker.
func firecrackerConfig(r *fuzzRun) []string {
	var fc hypervisors.FirecrackerConfig
	if !r.done() || r.fcConfig == nil || json.Unmarshal(r.fcConfig, &fc) != nil {
		return nil
	}
	args, params := r.ms.ExecArgs, r.ms.GuestParams
	var v []string
	if fc.Source.ImagePath == "" || fc.Source.ImagePath != args.UnikernelPath {
		v = append(v, "kernel-path-differs")
	}
	if args.InitrdPath != "" && fc.Source.InitrdPath != args.InitrdPath {
		v = append(v, "initrd-missing")
	}
	if args.Net.TapDev != "" && (len(fc.NetIfs) == 0 || fc.NetIfs[0].HostIF != args.Net.TapDev) {
		v = append(v, "network-missing")
	}
	rootDev := slices.ContainsFunc(fc.Drives, func(d hypervisors.FirecrackerDrive) bool { return d.IsRootDev })
	for _, b := range params.Block {
		if !slices.ContainsFunc(fc.Drives, func(d hypervisors.FirecrackerDrive) bool { return d.HostPath == b.Source }) {
			v = append(v, "block-missing")
		}
		if b.MountPoint == "/" && !rootDev {
			v = append(v, "rootfs-block-not-marked-root-device")
		}
	}
	switch {
	case fc.Machine.MemSizeMiB < 1:
		v = append(v, "memory-zero")
	case args.MemSizeB >= 1<<20 && fc.Machine.MemSizeMiB<<20 > args.MemSizeB:
		v = append(v, "memory-over-limit")
	}
	return v
}

// unikraftArgsEnv: a Unikraft guest gets the container's args and env.
func unikraftArgsEnv(r *fuzzRun) []string {
	uk, ok := r.guest.(*unikernels.Unikraft)
	if !r.done() || !ok || !r.engineShaped() {
		return nil
	}
	var v []string
	toks := ukArgParse(r.command)
	if len(toks) > unikraftMaxTokens {
		v = append(v, "too-many-tokens-guest-does-not-boot")
	}
	env, app := ukLibparamList(toks, "env.vars")
	if !slices.Equal(env, uk.Env) {
		v = append(v, "env-differs")
	}
	if !slices.Equal(app, r.ms.GuestParams.CmdLine) {
		v = append(v, "args-differ")
	}
	return v
}

// rumprunArgsEnv: a Rumprun guest gets the container's args and env.
func rumprunArgsEnv(r *fuzzRun) []string {
	rr, ok := r.guest.(*unikernels.Rumprun)
	if !r.done() || !ok || !r.engineShaped() {
		return nil
	}
	args, env := rumprunView(r.command)
	var v []string
	if !slices.Equal(args, r.ms.GuestParams.CmdLine) {
		v = append(v, "args-differ")
	}
	if !slices.Equal(env, rr.Envs) {
		v = append(v, "env-differs")
	}
	return v
}

// bootParamNames are the params urunc itself sets on a Linux guest; an env entry with one
// of these names overrides urunc's value.
var bootParamNames = map[string]bool{
	"panic": true, "console": true, "root": true, "rootfstype": true,
	"rootflags": true, "ip": true, "init": true, "rdinit": true,
}

// linuxCmdline: a Linux guest's init gets the container's args and env through the kernel command line.
func linuxCmdline(r *fuzzRun) []string {
	if _, ok := r.guest.(*unikernels.Linux); !r.done() || !ok || !r.engineShaped() {
		return nil
	}
	cmd, args, env := r.command, r.ms.GuestParams.CmdLine, r.u.Spec.Process.Env
	var v []string
	if len(cmd) > linuxCmdlineMax {
		v = append(v, "cmdline-over-2047-bytes-truncated")
	}
	if n := r.host & 3; n != 0 {
		net := fuzzNets[n-1]
		want := "ip=" + net.IP + "::" + net.Gateway + ":" + net.Mask + ":urunc:eth0:off"
		toks, _, _, _ := kernelParse(cmd)
		switch {
		case !strings.Contains(cmd, want):
			v = append(v, "ip-param-missing")
		case !slices.Contains(toks, want):
			v = append(v, "ip-param-split")
		}
	}
	boot, initArgs, sawDashes, cut := kernelParse(cmd)
	if !sawDashes {
		return append(v, "no-init-separator")
	}
	if cut {
		v = append(v, "init-args-cut-by-double-dash")
	}
	if len(initArgs) > kernelInitArgLimit {
		v = append(v, "too-many-init-args-kernel-panics")
	}
	if !slices.Contains(boot, "init="+args[0]) && !slices.Contains(boot, "rdinit="+args[0]) {
		v = append(v, "init-path-mangled")
	}
	if !cut && !slices.Equal(initArgs, args[1:]) {
		v = append(v, "init-args-differ")
	}
	last := map[string]int{}
	for i, e := range env {
		name, _, _ := strings.Cut(e, "=")
		last[name] = i
	}
	extra := len(last)
	for _, builtin := range []string{"HOME", "TERM"} {
		if _, ok := last[builtin]; ok {
			extra--
		}
	}
	if extra >= kernelInitArgLimit {
		v = append(v, "too-many-env-kernel-panics")
	}
	seen := map[string]bool{}
	for i, e := range env {
		name, _, _ := strings.Cut(e, "=")
		class := ""
		switch {
		case last[name] != i:
		case bootParamNames[name]:
			class = "env-overrides-boot-param"
		case strings.Contains(name, "."):
			class = "env-with-dot-dropped-as-module-param"
		case !slices.Contains(boot, e):
			class = "env-not-one-kernel-param"
		}
		if class != "" && !seen[class] {
			seen[class] = true
			v = append(v, class)
		}
	}
	return v
}
