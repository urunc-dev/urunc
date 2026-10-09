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

//go:build darwin

package hypervisors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestHviDarwinGenericContainerBoot(t *testing.T) {
	hvi := NewHviDarwin("/opt/hvi")
	args := types.ExecArgs{
		ContainerID:   "alpine-test",
		KernelPath:    "/host/Image",
		InitrdPath:    "/instance/container-initrd",
		Command:       "rdinit=/init console=ttyAMA0",
		MemSizeB:      512 << 20,
		VCPUs:         2,
		AgentSockPath: "/instance/agent.sock",
		Net:           types.NetDevParams{TapDev: "en0"},
		Sharedfs: types.SharedfsParams{
			Type: "virtiofs", Path: "/store/alpine/rootfs", Tag: "rootfs", ReadOnly: true,
		},
		SharedDirs: []types.SharedDirParams{
			{Path: "/host/config", Tag: "share0", ReadOnly: true},
			{Path: "/host/models", Tag: "share1"},
		},
	}
	argv, err := hvi.BuildExecCmd(args, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ") + " "
	for _, want := range []string{
		"/opt/hvi boot",
		"--kernel /host/Image",
		"--initramfs /instance/container-initrd",
		"--fs tag=rootfs,path=/store/alpine/rootfs,readonly=on",
		"--fs tag=share0,path=/host/config,readonly=on",
		"--fs tag=share1,path=/host/models ",
		"--cmdline rdinit=/init console=ttyAMA0",
		"--agent-sock /instance/agent.sock",
		"--net",
		"--sandbox-id alpine-test",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in HVI command:\n%s", want, joined)
		}
	}
}

func TestHviDarwinWritableAndDuplicateShares(t *testing.T) {
	hvi := NewHviDarwin("/opt/hvi")
	argv, err := hvi.BuildExecCmd(types.ExecArgs{
		KernelPath: "/host/Image",
		Sharedfs:   types.SharedfsParams{Path: "/host/rootfs"},
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatalf("writable export: %v", err)
	}
	if got := strings.Join(argv, " ") + " "; !strings.Contains(got, "--fs tag=rootfs,path=/host/rootfs ") {
		t.Fatalf("writable root export missing from %s", got)
	}
	argv, err = hvi.BuildExecCmd(types.ExecArgs{
		KernelPath: "/host/Image",
		SharedDirs: []types.SharedDirParams{{Path: "/host/extra", Tag: "extra"}},
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatalf("writable additional export: %v", err)
	}
	if got := strings.Join(argv, " ") + " "; !strings.Contains(got, "--fs tag=extra,path=/host/extra ") {
		t.Fatalf("writable additional export missing from %s", got)
	}
	_, err = hvi.BuildExecCmd(types.ExecArgs{
		KernelPath: "/host/Image",
		Sharedfs:   types.SharedfsParams{Path: "/host/rootfs", Tag: "same", ReadOnly: true},
		SharedDirs: []types.SharedDirParams{{Path: "/host/extra", Tag: "same", ReadOnly: true}},
	}, &fakeUnikernel{})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate tag: got %v", err)
	}
}

// A workload running as an unprivileged user gets each export owned by it,
// on the export itself. The process-wide --fs-uid/--fs-gid pair is
// deprecated, and hvi prints a note for each use.
func TestHviDarwinExportOwner(t *testing.T) {
	hvi := NewHviDarwin("/opt/hvi")
	argv, err := hvi.BuildExecCmd(types.ExecArgs{
		KernelPath: "/host/Image",
		GuestUID:   1000,
		GuestGID:   1000,
		Sharedfs:   types.SharedfsParams{Path: "/host/rootfs", Tag: "rootfs", ReadOnly: true},
		SharedDirs: []types.SharedDirParams{{Path: "/host/work", Tag: "work"}},
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ") + " "
	for _, want := range []string{
		"--fs tag=rootfs,path=/host/rootfs,readonly=on,uid=1000,gid=1000 ",
		"--fs tag=work,path=/host/work,uid=1000,gid=1000 ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %s", want, got)
		}
	}
	for _, old := range []string{"--fs-uid", "--fs-gid", "--share-ro", "--share-rw"} {
		if strings.Contains(got, old) {
			t.Errorf("deprecated flag %s in %s", old, got)
		}
	}

	// root is hvi's default owner, so it is not spelled out.
	argv, err = hvi.BuildExecCmd(types.ExecArgs{
		KernelPath: "/host/Image",
		Sharedfs:   types.SharedfsParams{Path: "/host/rootfs", Tag: "rootfs"},
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(argv, " "); strings.Contains(got, "uid=") || strings.Contains(got, "gid=") {
		t.Fatalf("root owner should be left to hvi's default: %s", got)
	}
}

// --fs is split on commas with no escape, so a comma in a path or tag would
// be parsed as the next option.
func TestHviDarwinExportRefusesComma(t *testing.T) {
	hvi := NewHviDarwin("/opt/hvi")
	for _, share := range []types.SharedfsParams{
		{Path: "/host/a,b", Tag: "rootfs"},
		{Path: "/host/rootfs", Tag: "a,b"},
	} {
		_, err := hvi.BuildExecCmd(types.ExecArgs{KernelPath: "/host/Image", Sharedfs: share}, &fakeUnikernel{})
		if err == nil || !strings.Contains(err.Error(), "comma") {
			t.Errorf("%+v: got %v, want a comma refusal", share, err)
		}
	}
}

func TestHviDarwinGatewayNetwork(t *testing.T) {
	hvi := NewHviDarwin("/opt/hvi")
	argv, err := hvi.BuildExecCmd(types.ExecArgs{
		KernelPath: "/host/Image",
		Net: types.NetDevParams{
			TapDev:     "en0",
			UnixSocket: "/run/hull/gateway.qemu",
		},
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	if !strings.Contains(got, "--net-gateway /run/hull/gateway.qemu") {
		t.Fatalf("gateway network missing from %s", got)
	}
	if strings.Contains(got, " --net ") || strings.HasSuffix(got, " --net") {
		t.Fatalf("built-in network must not accompany gateway network: %s", got)
	}
}

// The gateway keys its DHCP leases by MAC, so each guest's MAC goes to hvi,
// with the gateway and with the built-in network alike.
func TestHviDarwinNetMAC(t *testing.T) {
	hvi := NewHviDarwin("/opt/hvi")
	for _, net := range []types.NetDevParams{
		{UnixSocket: "/run/hull/gateway.qemu", MAC: "02:00:00:00:00:01"},
		{TapDev: "en0", MAC: "02:00:00:00:00:01"},
	} {
		argv, err := hvi.BuildExecCmd(types.ExecArgs{KernelPath: "/host/Image", Net: net}, &fakeUnikernel{})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(argv, " ") + " "; !strings.Contains(got, "--net-mac 02:00:00:00:00:01 ") {
			t.Errorf("%+v: --net-mac missing from %s", net, got)
		}
	}
	argv, err := hvi.BuildExecCmd(types.ExecArgs{KernelPath: "/host/Image"}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--net", "--net-gateway", "--net-mac"} {
		for _, a := range argv {
			if a == flag {
				t.Errorf("no network asked for, but %s in %v", flag, argv)
			}
		}
	}
}

// KernelPath wins, UnikernelPath stands in for it, and with neither there is
// nothing to boot.
func TestHviDarwinKernel(t *testing.T) {
	hvi := NewHviDarwin("/opt/hvi")
	argv, err := hvi.BuildExecCmd(types.ExecArgs{UnikernelPath: "/bundle/unikernel"}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/opt/hvi", "boot", "--kernel", "/bundle/unikernel", "--mem-mib", "256", "--cpus", "1"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", argv, want)
	}
	argv, err = hvi.BuildExecCmd(types.ExecArgs{KernelPath: "/host/Image", UnikernelPath: "/bundle/unikernel"}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	if argv[3] != "/host/Image" {
		t.Fatalf("KernelPath must win over UnikernelPath: %v", argv)
	}
	if _, err := hvi.BuildExecCmd(types.ExecArgs{}, &fakeUnikernel{}); err == nil {
		t.Fatal("expected an error without a kernel")
	}
}

func TestHviDarwinCapabilities(t *testing.T) {
	hvi := NewHviDarwin("/opt/hvi")
	if hvi.UsesKVM() {
		t.Error("hvi on darwin does not use KVM")
	}
	if !hvi.SupportsSharedfs("virtiofs") || hvi.SupportsSharedfs("9pfs") {
		t.Error("hvi on darwin shares only virtiofs")
	}
	if hvi.SupportsControlSocket() {
		t.Error("hvi on darwin has no control socket")
	}
	if err := hvi.Signal(0, 0); err == nil {
		t.Error("Signal must refuse pid 0")
	}
}

func TestHviDarwinOk(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hvi")
	if err := os.WriteFile(exe, nil, 0o755); err != nil { //nolint:gosec // a stub executable
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewHviDarwin(exe).Ok(); err != nil {
		t.Errorf("executable hvi: %v", err)
	}
	for _, p := range []string{plain, dir, filepath.Join(dir, "missing")} {
		if err := NewHviDarwin(p).Ok(); err == nil {
			t.Errorf("%s: expected Ok to fail", p)
		}
	}
}

// NewVMM("hvi") takes the configured path first, then PATH.
func TestNewVMMHviDarwin(t *testing.T) {
	vmm, err := NewVMM(HviVmm, map[string]types.MonitorConfig{"hvi": {BinaryPath: "/opt/hvi"}})
	if err != nil {
		t.Fatal(err)
	}
	h, ok := vmm.(*HviDarwin)
	if !ok {
		t.Fatalf("NewVMM(hvi) must return a *HviDarwin, got %T", vmm)
	}
	if h.Path() != "/opt/hvi" {
		t.Fatalf("configured path not used: %s", h.Path())
	}

	dir := t.TempDir()
	exe := filepath.Join(dir, HviBinary)
	if err := os.WriteFile(exe, nil, 0o755); err != nil { //nolint:gosec // a stub executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	vmm, err = NewVMM(HviVmm, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vmm.Path(); got != exe {
		t.Fatalf("PATH lookup: got %s, want %s", got, exe)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := NewVMM(HviVmm, nil); err == nil || !strings.Contains(err.Error(), "hvi not found") {
		t.Fatalf("no hvi anywhere: got %v", err)
	}
}

func TestHviDarwinDisks(t *testing.T) {
	cases := []struct {
		name string
		args types.ExecArgs
		want []string
	}{
		{
			name: "no disk",
			args: types.ExecArgs{},
			want: nil,
		},
		{
			name: "legacy BlockDevPath is one writable disk",
			args: types.ExecArgs{BlockDevPath: "/instance/root.ext4"},
			want: []string{"--disk", "path=/instance/root.ext4,serial=disk0"},
		},
		{
			// The serial is the position across both flags, so a read-only
			// lower first and a writable upper second must stay in that order.
			name: "BlockDevs keep their order and mode",
			args: types.ExecArgs{BlockDevs: []types.BlockDevSpec{
				{Path: "/store/lower.ext4", ReadOnly: true},
				{Path: "/instance/upper.ext4"},
			}},
			want: []string{
				"--disk", "path=/store/lower.ext4,readonly=on,serial=disk0",
				"--disk", "path=/instance/upper.ext4,serial=disk1",
			},
		},
		{
			name: "BlockDevs win over BlockDevPath",
			args: types.ExecArgs{
				BlockDevPath: "/instance/root.ext4",
				BlockDevs:    []types.BlockDevSpec{{Path: "/store/lower.ext4", ReadOnly: true}},
			},
			want: []string{"--disk", "path=/store/lower.ext4,readonly=on,serial=disk0"},
		},
	}
	hvi := NewHviDarwin("/opt/hvi")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.args.KernelPath = "/host/Image"
			argv, err := hvi.BuildExecCmd(tc.args, &fakeUnikernel{})
			if err != nil {
				t.Fatal(err)
			}
			got := diskArgs(argv)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("disks: got %q, want %q\nargv: %v", got, tc.want, argv)
			}
		})
	}
}

func TestHviDarwinDisksRejected(t *testing.T) {
	cases := []struct {
		name  string
		disks []types.BlockDevSpec
		want  string
	}{
		{
			name:  "empty path",
			disks: []types.BlockDevSpec{{Path: "/store/lower.ext4", ReadOnly: true}, {}},
			want:  "hvi: block device 1 has no path",
		},
		{
			name: "duplicate path",
			disks: []types.BlockDevSpec{
				{Path: "/instance/disk.ext4", ReadOnly: true},
				{Path: "/instance/disk.ext4"},
			},
			want: `hvi: duplicate block device "/instance/disk.ext4"`,
		},
		{
			// hvi splits the option string on commas, with no escape.
			name:  "comma in path",
			disks: []types.BlockDevSpec{{Path: "/store/a,b.ext4", ReadOnly: true}},
			want:  `hvi: block device "/store/a,b.ext4": a comma cannot be passed in --disk`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewHviDarwin("/opt/hvi").BuildExecCmd(types.ExecArgs{
				KernelPath: "/host/Image",
				BlockDevs:  tc.disks,
			}, &fakeUnikernel{})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got error %v, want %q", err, tc.want)
			}
		})
	}
}

// Disks and the root share are independent on hvi: an overlay boot passes a
// read-only lower and a writable upper next to the virtio-fs shares.
func TestHviDarwinDisksWithShares(t *testing.T) {
	argv, err := NewHviDarwin("/opt/hvi").BuildExecCmd(types.ExecArgs{
		KernelPath: "/host/Image",
		Sharedfs:   types.SharedfsParams{Path: "/store/rootfs", Tag: "rootfs", ReadOnly: true},
		SharedDirs: []types.SharedDirParams{{Path: "/host/home", Tag: "share0"}},
		BlockDevs: []types.BlockDevSpec{
			{Path: "/store/lower.ext4", ReadOnly: true},
			{Path: "/instance/upper.ext4"},
		},
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"--disk path=/store/lower.ext4,readonly=on,serial=disk0 --disk path=/instance/upper.ext4,serial=disk1",
		"--fs tag=rootfs,path=/store/rootfs,readonly=on",
		"--fs tag=share0,path=/host/home",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in HVI command:\n%s", want, joined)
		}
	}
}

// diskArgs returns the disk flags in argv with their values, in order, so a
// test can check both the mode of each disk and the order of the serials.
func diskArgs(argv []string) []string {
	var out []string
	for i := 0; i < len(argv)-1; i++ {
		switch argv[i] {
		case "--disk", "--disk-ro", "--rootfs":
			out = append(out, argv[i], argv[i+1])
			i++
		}
	}
	return out
}
