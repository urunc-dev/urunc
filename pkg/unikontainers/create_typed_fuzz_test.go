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
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

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

// FuzzCreateTyped drives FuzzCreate's pipeline from typed values instead of bytes. It builds a
// well-formed config.json, and the image's urunc.json if viaJSON, for one of the supported guest
// and monitor pairs, so that every input gets past the first checks that the raw bytes of FuzzCreate
// rarely do. host is FuzzCreate's host byte. sel adds a vAccel annotation (bit 3), a Solo5 block and
// net device name (bits 4 and 5) and a queue-proxy container (bit 6). The lists in args, env and
// mountDests are separated by NUL, which an execve string cannot hold.
func FuzzCreateTyped(f *testing.F) {
	logrus.SetOutput(io.Discard)
	f.Cleanup(func() { logrus.SetOutput(os.Stderr) })
	env := newFuzzEnv(f)
	pairs := typedPairs()
	idx := func(guest string, vmm hypervisors.VmmType) uint8 {
		return uint8(slices.IndexFunc(pairs, func(p guestMonitorPair) bool { return p.guest == guest && p.monitor == vmm })) //nolint:gosec // a handful of pairs
	}
	f.Add(idx(unikernels.LinuxUnikernel, hypervisors.QemuVmm), false, "/.boot/kernel", "", "", "", "", "",
		"/urunit\x00/usr/sbin/nginx\x00-g\x00daemon off;", "PATH=/usr/bin\x00NGINX_VERSION=1.27", "/", uint32(0), uint32(0),
		int64(512<<20), true, "/etc/resolv.conf", uint8(0), uint8(1))
	f.Add(idx(unikernels.UnikraftUnikernel, hypervisors.QemuVmm), false, "/unikernel/app", "/unikernel/initrd", "", "", "", "0.18.0",
		"/app\x00arg", "A=1", "/", uint32(0), uint32(0), int64(128<<20), true, "/boot/cfg\x00/data/cfg", uint8(0), uint8(1))
	f.Add(idx(unikernels.RumprunUnikernel, hypervisors.HvtVmm), true, "/unikernel/redis.hvt", "", "/data.img", "/data", "", "",
		"redis-server\x00/data/redis.conf", "A=1", "/", uint32(0), uint32(0), int64(0), false, "", uint8(0x30), uint8(1))
	f.Add(idx(unikernels.MirageUnikernel, hypervisors.SptVmm), false, "/unikernel/net.spt", "", "", "", "", "",
		"net", "", "/", uint32(0), uint32(0), int64(64<<20), false, "", uint8(0), uint8(1))
	f.Add(idx(unikernels.LinuxUnikernel, hypervisors.QemuVmm), false, "/.boot/kernel", "", "", "", "", "",
		"/bin/queue-proxy", "PORT=8012", "/", uint32(0), uint32(0), int64(0), false, "", uint8(0x40), uint8(0))

	f.Fuzz(func(t *testing.T, pair uint8, viaJSON bool, bin, initrd, block, blkMnt, mountRootfs, version,
		args, envList, cwd string, uid, gid uint32, memLimit int64, seccomp bool, mountDests string, sel, host uint8) {
		p := pairs[int(pair)%len(pairs)]
		spec := specs.Spec{
			Version: specs.Version,
			Root:    &specs.Root{Path: "rootfs"},
			Process: &specs.Process{Args: strings.Split(args, "\x00"), Cwd: cwd, User: specs.User{UID: uid, GID: gid}},
			Linux:   &specs.Linux{},
		}
		if envList != "" {
			spec.Process.Env = strings.Split(envList, "\x00")
		}
		if memLimit != 0 {
			spec.Linux.Resources = &specs.LinuxResources{Memory: &specs.LinuxMemory{Limit: &memLimit}}
		}
		if seccomp { // an engine sets a capability set alongside a seccomp profile
			spec.Linux.Seccomp = &specs.LinuxSeccomp{DefaultAction: specs.ActAllow}
			caps := []string{"CAP_CHOWN", "CAP_NET_RAW"}
			spec.Process.Capabilities = &specs.LinuxCapabilities{Bounding: caps, Effective: caps, Permitted: caps}
		}
		if mountDests != "" {
			for i, dest := range strings.Split(mountDests, "\x00") {
				spec.Mounts = append(spec.Mounts, specs.Mount{Type: "bind", Source: "@BUNDLE@/vol" + strconv.Itoa(i),
					Destination: dest, Options: []string{"rbind", "ro"}})
			}
		}
		solo5 := p.monitor == hypervisors.HvtVmm || p.monitor == hypervisors.SptVmm
		conf := UnikernelConfig{
			UnikernelType: p.guest, Hypervisor: string(p.monitor), UnikernelBinary: bin, UnikernelVersion: version,
			Initrd: initrd, Block: block, BlkMntPoint: blkMnt, MountRootfs: mountRootfs,
		}
		if solo5 && sel&0x20 != 0 {
			conf.NetDev = "virtio"
		}
		if solo5 && sel&0x10 != 0 {
			conf.BlkDev = "virtio"
		}
		if p.monitor == hypervisors.QemuVmm && sel&0x08 != 0 {
			conf.VAccel, conf.RPCAddress = "vsock", "vsock://2:1234"
		}
		var uruncJSON []byte
		if viaJSON {
			enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
			uruncJSON, _ = json.Marshal(UnikernelConfig{
				UnikernelType: enc(conf.UnikernelType), Hypervisor: enc(conf.Hypervisor), UnikernelBinary: enc(conf.UnikernelBinary),
				UnikernelVersion: enc(conf.UnikernelVersion), Initrd: enc(conf.Initrd), Block: enc(conf.Block),
				BlkMntPoint: enc(conf.BlkMntPoint), MountRootfs: enc(conf.MountRootfs),
			})
		} else {
			spec.Annotations = conf.Map()
		}
		if sel&0x40 != 0 { // a knative queue-proxy sidecar: New rewrites its config.json
			if spec.Annotations == nil {
				spec.Annotations = map[string]string{}
			}
			spec.Annotations[annotCRICntrName] = criQueueProxyCntr
			spec.Process.Env = append(spec.Process.Env, `SERVING_READINESS_PROBE={"tcpSocket":{"port":8012},"host":"127.0.0.1"}`)
		}
		config, err := json.Marshal(spec)
		if err != nil {
			return
		}
		env.run(t, bytes.Join([][]byte{config, uruncJSON, {host}}, []byte{0}))
	})
}
