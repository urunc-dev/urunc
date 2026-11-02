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
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// fuzzNets are shaped like what the CNI hands urunc, which is never malformed.
var fuzzNets = []types.NetDevParams{
	{IP: "10.0.0.5", Gateway: "10.0.0.1", Mask: "255.255.255.0", MAC: "aa:bb:cc:dd:ee:ff", TapDev: "tap0_urunc", MTU: 1500},
	{IP: "192.168.1.20", Gateway: "192.168.1.1", Mask: "255.255.255.0", MAC: "02:42:ac:11:00:02", TapDev: "tap0_urunc", MTU: 1500},
	{IP: "172.16.0.9", Gateway: "172.16.31.254", Mask: "255.240.0.0", MAC: "0a:00:27:00:00:09", TapDev: "tap0_urunc", MTU: 9000},
}

// argvMonitors are the monitors that get their settings on the command line.
var argvMonitors = []hypervisors.VmmType{hypervisors.QemuVmm, hypervisors.CloudHypervisorVmm, hypervisors.HvtVmm, hypervisors.SptVmm}

// monitorStart is what the monitor process has built just before it would run the monitor.
type monitorStart struct {
	ms       monitorSpec
	guest    types.Unikernel
	argv     []string
	fcConfig []byte // Firecracker's config, which it takes in place of arguments
}

// startMonitor does what the monitor process does with its spec, up to the monitor's command,
// with the network that bits 0-1 of the host byte choose.
func startMonitor(ms monitorSpec, host byte) (st monitorStart, err error) {
	st.ms = ms
	if st.guest, err = unikernels.New(ms.UnikernelType); err != nil {
		return st, err
	}
	vmmType := hypervisors.VmmType(ms.MonitorType)
	vmm, err := hypervisors.NewVMM(vmmType, map[string]types.MonitorConfig{ms.MonitorType: ms.MonitorCfg})
	if err != nil {
		return st, err
	}
	if n := host & 3; n != 0 {
		net := fuzzNets[n-1]
		net.DNSServer = ms.DNSServer // SetupNet does not resolve DNS; the spec carries it
		if !vmm.UsesTUNTAP() {       // SetupNet creates no tap for such a monitor
			net.TapDev = ""
		}
		st.ms.ExecArgs.Net, st.ms.GuestParams.Net = net, net
	}
	// The same steps, in the same order, as runMonitor.
	args, params := &st.ms.ExecArgs, &st.ms.GuestParams
	if args.UnikernelPath, err = confineToContainerRootfs(args.UnikernelPath); err != nil {
		return st, err
	}
	if args.InitrdPath, err = confineToContainerRootfs(args.InitrdPath); err != nil {
		return st, err
	}
	if args.SnapshotPath, err = confineToContainerRootfs(args.SnapshotPath); err != nil {
		return st, err
	}
	if params.Block, err = confineBlockSources(params.Block); err != nil {
		return st, err
	}
	if args.Command, err = buildUnikernelCommand(st.guest, *params); err != nil {
		return st, err
	}
	if fc, ok := vmm.(*hypervisors.Firecracker); ok { // its config is built, not written to a fixed path
		st.argv, st.fcConfig, err = fc.BuildConfig(*args, st.guest)
		return st, err
	}
	st.argv, err = vmm.BuildExecCmd(*args, st.guest)
	return st, err
}

// checkMonitorSpec fails if the monitor process would read other args or env than the container's.
func checkMonitorSpec(t *testing.T, spec *specs.Spec, ms monitorSpec) {
	if !slices.Equal(ms.GuestParams.CmdLine, spec.Process.Args) {
		t.Fatalf("the monitor spec has args %q, the container's are %q", ms.GuestParams.CmdLine, spec.Process.Args)
	}
	if !slices.Equal(ms.GuestParams.EnvVars, spec.Process.Env) {
		t.Fatalf("the monitor spec has env %q, the container's is %q", ms.GuestParams.EnvVars, spec.Process.Env)
	}
}

// checkMonitorStart fails if something the container asked for (nameserver, network, seccomp,
// memory, initrd, shared rootfs) does not reach the guest command line or the monitor's argv.
func checkMonitorStart(t *testing.T, spec *specs.Spec, ms monitorSpec, rootfsType string, host byte, argv []string) {
	checkUnikraftNameserver(t, spec, ms, host)
	if slices.Contains(argvMonitors, hypervisors.VmmType(ms.MonitorType)) {
		checkMonitorArgv(t, spec, ms, rootfsType, argv)
	}
}

// checkUnikraftNameserver fails if a Unikraft command line with netdev.ip= lacks the nameserver of a
// usable resolv.conf. Older versions have no DNS argument, so they are not held to it.
func checkUnikraftNameserver(t *testing.T, spec *specs.Spec, ms monitorSpec, host byte) {
	cmd := ms.ExecArgs.Command
	if ms.UnikernelType != unikernels.UnikraftUnikernel || ms.ExecArgs.Net.TapDev == "" ||
		!strings.Contains(cmd, "netdev.ip=") || !bindsResolv(spec.Mounts) {
		return
	}
	if bytes.Contains(fuzzResolv[host>>6], []byte(fuzzNameserver)) && !strings.Contains(cmd, fuzzNameserver) {
		t.Fatalf("the Unikraft command line %q has not got the nameserver of resolv.conf", cmd)
	}
}

// checkMonitorArgv fails if the argv of a monitor that is configured by arguments lacks the
// network device, memory or initrd of the container, or the seccomp and rootfs urunc chose.
func checkMonitorArgv(t *testing.T, spec *specs.Spec, ms monitorSpec, rootfsType string, argv []string) {
	vmm, cmd := hypervisors.VmmType(ms.MonitorType), strings.Join(argv, " ")
	if tap := ms.ExecArgs.Net.TapDev; tap != "" && !strings.Contains(cmd, tap) {
		t.Fatalf("%s is not given the network device %s: %q", vmm, tap, argv)
	}
	if mem, ok := monitorMemMiB(vmm, argv); !ok || mem < 1 {
		t.Fatalf("%s is given no memory: %q", vmm, argv)
	}
	if initrd := ms.ExecArgs.InitrdPath; initrd != "" && (vmm == hypervisors.QemuVmm || vmm == hypervisors.CloudHypervisorVmm) && !slices.Contains(argv, initrd) {
		t.Fatalf("%s is not given the initrd %s: %q", vmm, initrd, argv)
	}
	if spec.Linux.Seccomp != nil {
		switch {
		case vmm == hypervisors.QemuVmm && !strings.Contains(cmd, "--sandbox on"):
			t.Fatalf("qemu runs without its sandbox though the container has a seccomp profile: %q", argv)
		case vmm == hypervisors.CloudHypervisorVmm && !strings.Contains(cmd, "--seccomp true"):
			t.Fatalf("cloud-hypervisor runs without seccomp though the container has a profile: %q", argv)
		}
	}
	virtiofs, plan9 := rootfsType == "virtiofs", rootfsType == "9pfs"
	switch {
	case vmm == hypervisors.QemuVmm && virtiofs && !strings.Contains(cmd, "vhost-user-fs"),
		vmm == hypervisors.QemuVmm && plan9 && !strings.Contains(cmd, "virtio-9p"),
		vmm == hypervisors.CloudHypervisorVmm && (virtiofs || plan9) && !strings.Contains(cmd, "--fs "):
		t.Fatalf("%s is not given the %s rootfs urunc chose: %q", vmm, rootfsType, argv)
	}
}

// bindsResolv reports whether the container has a resolv.conf bind-mounted from the engine's bundle.
func bindsResolv(mounts []specs.Mount) bool {
	return slices.ContainsFunc(mounts, func(m specs.Mount) bool {
		return m.Type == "bind" && m.Source != "/proc" && filepath.Clean(m.Destination) == "/etc/resolv.conf"
	})
}

// monitorMemMiB returns the guest memory, in MiB, that a monitor's argv asks for.
func monitorMemMiB(vmm hypervisors.VmmType, argv []string) (uint64, bool) {
	parse := func(s string) (uint64, bool) {
		n, err := strconv.ParseUint(s, 10, 64)
		return n, err == nil
	}
	for i, a := range argv {
		switch {
		case vmm == hypervisors.QemuVmm && a == "-m" && i+1 < len(argv):
			return parse(strings.TrimSuffix(argv[i+1], "M"))
		case vmm == hypervisors.CloudHypervisorVmm && a == "--memory" && i+1 < len(argv):
			size, _, _ := strings.Cut(strings.TrimPrefix(argv[i+1], "size="), ",")
			return parse(strings.TrimSuffix(size, "M"))
		case (vmm == hypervisors.HvtVmm || vmm == hypervisors.SptVmm) && strings.HasPrefix(a, "--mem="):
			return parse(strings.TrimPrefix(a, "--mem="))
		}
	}
	return 0, false
}
