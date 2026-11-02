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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	specs "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/internal/fuzzseed"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// seedRow is a FuzzCreate seed of seeds.toml: the base container with the values it sets, on one
// guest and monitor pair, or on all of them for "*".
type seedRow struct {
	Pair                          string
	JSON, NoResolv                bool // urunc.json instead of annotations; no resolv.conf mount
	Bin, Initrd, Block, BlkMnt    string
	MountRootfs, Version, Guest   string   // Guest replaces the pair's guest
	Mounts                        []string // destinations of extra file mounts
	Args, Env                     []string // of the container process; /app arg and PATH when empty
	MemMiB                        int64
	Seccomp, Confine              bool // a seccomp profile with capabilities; namespaces, masked paths and so on
	Solo5Devs, VAccel, QueueProxy bool
	Host                          byte
	UruncJSON, BadKey             string // the urunc.json part, verbatim; a key of it that is not base64
}

// pairs returns the guest and monitor pairs the row is for.
func (r seedRow) pairs() []guestMonitorPair {
	if r.Pair == "*" {
		return typedPairs()
	}
	monitor, guest, _ := strings.Cut(r.Pair, "/")
	p := guestMonitorPair{hypervisors.VmmType(monitor), guest}
	if !supportedGuestMonitorPairs[p] {
		panic(fmt.Sprintf("seeds.toml: %q is not a supported pair", r.Pair))
	}
	return []guestMonitorPair{p}
}

// spec returns the typed values of the row on the pair.
func (r seedRow) spec(p guestMonitorPair) typedSpec {
	if r.Guest != "" {
		p.guest = r.Guest
	}
	if r.Bin == "" {
		r.Bin = "/unikernel/app"
		if p.guest == unikernels.LinuxUnikernel {
			r.Bin = "/.boot/kernel"
		}
	}
	dests := []string{"/etc/resolv.conf"} // an engine bind-mounts the container's resolv.conf from the bundle
	if r.NoResolv {
		dests = nil
	}
	dests = append(dests, r.Mounts...)
	var sel uint8
	if r.VAccel {
		sel |= selVAccel
	}
	if r.Solo5Devs {
		sel |= selBlkDev | selNetDev
	}
	if r.QueueProxy {
		sel |= selQueueProxy
	}
	if r.Confine {
		sel |= selConfine
	}
	args, env := r.Args, r.Env
	if len(args) == 0 {
		args = []string{"/app", "arg"}
	}
	if len(env) == 0 {
		env = []string{"PATH=/usr/bin"}
	}
	return typedSpec{
		pair: p, viaJSON: r.JSON, bin: r.Bin, initrd: r.Initrd, block: r.Block, blkMnt: r.BlkMnt,
		mountRootfs: r.MountRootfs, version: r.Version, args: strings.Join(args, "\x00"), env: strings.Join(env, "\x00"), cwd: "/",
		memLimit: r.MemMiB << 20, seccomp: r.Seccomp, mountDests: strings.Join(dests, "\x00"), sel: sel, host: r.Host,
	}
}

// input builds the bytes FuzzCreate takes for the row on the pair.
func (r seedRow) input(p guestMonitorPair) []byte {
	in := typedInput(r.spec(p))
	parts := bytes.Split(in, []byte{0}) // config.json, urunc.json, the host byte
	if r.BadKey != "" {
		var conf map[string]string
		_ = json.Unmarshal(parts[1], &conf)
		conf[annotUruncPrefix+r.BadKey] = "!!!"
		parts[1], _ = json.Marshal(conf)
	}
	if r.UruncJSON != "" {
		parts[1] = []byte(r.UruncJSON)
	}
	return bytes.Join(parts, []byte{0})
}

// fuzzCreateSeeds returns FuzzCreate's seeds. They are built once: a libFuzzer build calls FuzzCreate
// for every input.
var fuzzCreateSeeds = sync.OnceValue(func() [][]byte {
	var seeds [][]byte
	for _, r := range fuzzseed.Load[seedRow]("FuzzCreate") {
		for _, p := range r.pairs() {
			seeds = append(seeds, r.input(p))
		}
	}
	return seeds
})

// typedPairs is supportedGuestMonitorPairs in a stable order.
func typedPairs() []guestMonitorPair {
	var pairs []guestMonitorPair
	for p := range supportedGuestMonitorPairs {
		pairs = append(pairs, p)
	}
	slices.SortFunc(pairs, func(a, b guestMonitorPair) int {
		return strings.Compare(string(a.monitor)+a.guest, string(b.monitor)+b.guest)
	})
	return pairs
}

// What typedSpec.sel switches on, in bits 3 to 7.
const (
	selVAccel uint8 = 1 << (iota + 3)
	selBlkDev
	selNetDev
	selQueueProxy
	selConfine
)

// typedSpec holds the values FuzzCreateTyped takes, in the order it takes them.
type typedSpec struct {
	pair                                             guestMonitorPair
	viaJSON                                          bool
	bin, initrd, block, blkMnt, mountRootfs, version string
	args, env, cwd                                   string
	uid, gid                                         uint32
	memLimit                                         int64
	seccomp                                          bool
	mountDests                                       string
	sel, host                                        uint8
}

// typedInput builds the bytes FuzzCreate takes: config.json, then urunc.json and the host byte.
func typedInput(s typedSpec) []byte {
	p := s.pair
	spec := specs.Spec{
		Version: specs.Version,
		Root:    &specs.Root{Path: "rootfs"},
		Process: &specs.Process{Args: strings.Split(s.args, "\x00"), Cwd: s.cwd, User: specs.User{UID: s.uid, GID: s.gid}},
		Linux:   &specs.Linux{},
	}
	if s.env != "" {
		spec.Process.Env = strings.Split(s.env, "\x00")
	}
	if s.memLimit != 0 {
		spec.Linux.Resources = &specs.LinuxResources{Memory: &specs.LinuxMemory{Limit: &s.memLimit}}
	}
	if s.seccomp { // an engine sets a capability set alongside a seccomp profile
		spec.Linux.Seccomp = &specs.LinuxSeccomp{DefaultAction: specs.ActAllow}
		caps := []string{"CAP_CHOWN", "CAP_NET_RAW"}
		spec.Process.Capabilities = &specs.LinuxCapabilities{Bounding: caps, Effective: caps, Permitted: caps}
	}
	if s.sel&selConfine != 0 { // namespaces, masked and read-only paths, no new privileges, an OOM score
		oom := 100
		spec.Linux.Namespaces = []specs.LinuxNamespace{{Type: specs.PIDNamespace}, {Type: specs.MountNamespace}}
		spec.Linux.MaskedPaths, spec.Linux.ReadonlyPaths = []string{"/proc/keys"}, []string{"/proc/sys"}
		spec.Process.NoNewPrivileges, spec.Process.OOMScoreAdj = true, &oom
	}
	if s.mountDests != "" {
		for i, dest := range strings.Split(s.mountDests, "\x00") {
			spec.Mounts = append(spec.Mounts, specs.Mount{Type: "bind", Source: "@BUNDLE@/vol" + strconv.Itoa(i),
				Destination: dest, Options: []string{"rbind", "ro"}})
		}
	}
	solo5 := p.monitor == hypervisors.HvtVmm || p.monitor == hypervisors.SptVmm
	conf := UnikernelConfig{
		UnikernelType: p.guest, Hypervisor: string(p.monitor), UnikernelBinary: s.bin, UnikernelVersion: s.version,
		Initrd: s.initrd, Block: s.block, BlkMntPoint: s.blkMnt, MountRootfs: s.mountRootfs,
	}
	if solo5 && s.sel&selNetDev != 0 {
		conf.NetDev = "virtio"
	}
	if solo5 && s.sel&selBlkDev != 0 {
		conf.BlkDev = "virtio"
	}
	if p.monitor == hypervisors.QemuVmm && s.sel&selVAccel != 0 {
		conf.VAccel, conf.RPCAddress = "vsock", "vsock://2:1234"
	}
	var uruncJSON []byte
	if s.viaJSON {
		enc := func(v string) string { return base64.StdEncoding.EncodeToString([]byte(v)) }
		uruncJSON, _ = json.Marshal(UnikernelConfig{
			UnikernelType: enc(conf.UnikernelType), Hypervisor: enc(conf.Hypervisor), UnikernelBinary: enc(conf.UnikernelBinary),
			UnikernelVersion: enc(conf.UnikernelVersion), Initrd: enc(conf.Initrd), Block: enc(conf.Block),
			BlkMntPoint: enc(conf.BlkMntPoint), MountRootfs: enc(conf.MountRootfs),
		})
	} else {
		spec.Annotations = conf.Map()
	}
	if s.sel&selQueueProxy != 0 { // a knative queue-proxy sidecar: New rewrites its config.json
		if spec.Annotations == nil {
			spec.Annotations = map[string]string{}
		}
		spec.Annotations[annotCRICntrName] = criQueueProxyCntr
		spec.Process.Env = append(spec.Process.Env, `SERVING_READINESS_PROBE={"tcpSocket":{"port":8012},"host":"127.0.0.1"}`)
	}
	config, _ := json.Marshal(spec) // a spec of plain values always marshals
	return bytes.Join([][]byte{config, uruncJSON, {s.host}}, []byte{0})
}
