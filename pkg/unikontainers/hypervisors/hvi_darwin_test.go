//go:build darwin

package hypervisors

import (
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
		Command:       "rdinit=/vz-init console=ttyAMA0",
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
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"/opt/hvi boot",
		"--kernel /host/Image",
		"--initramfs /instance/container-initrd",
		"--share-ro /store/alpine/rootfs rootfs",
		"--share-ro /host/config share0",
		"--share-rw /host/models share1",
		"--cmdline rdinit=/vz-init console=ttyAMA0",
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
	if got := strings.Join(argv, " "); !strings.Contains(got, "--share-rw /host/rootfs rootfs") {
		t.Fatalf("writable root export missing from %s", got)
	}
	argv, err = hvi.BuildExecCmd(types.ExecArgs{
		KernelPath: "/host/Image",
		SharedDirs: []types.SharedDirParams{{Path: "/host/extra", Tag: "extra"}},
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatalf("writable additional export: %v", err)
	}
	if got := strings.Join(argv, " "); !strings.Contains(got, "--share-rw /host/extra extra") {
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

// diskArgs returns the disk flags in argv with their values, in order, so a
// test can check both the mode of each disk and the order the serials follow.
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
			want: []string{"--disk", "/instance/root.ext4"},
		},
		{
			// The serial is the position across both flags, so a read-only
			// lower first and a writable upper second must stay in that order.
			name: "BlockDevs keep their order and mode",
			args: types.ExecArgs{BlockDevs: []types.BlockDevSpec{
				{Path: "/store/lower.ext4", ReadOnly: true},
				{Path: "/instance/upper.ext4"},
			}},
			want: []string{"--disk-ro", "/store/lower.ext4", "--disk", "/instance/upper.ext4"},
		},
		{
			name: "BlockDevs win over BlockDevPath",
			args: types.ExecArgs{
				BlockDevPath: "/instance/root.ext4",
				BlockDevs:    []types.BlockDevSpec{{Path: "/store/lower.ext4", ReadOnly: true}},
			},
			want: []string{"--disk-ro", "/store/lower.ext4"},
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
		"--disk-ro /store/lower.ext4 --disk /instance/upper.ext4",
		"--share-ro /store/rootfs rootfs",
		"--share-rw /host/home share0",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in HVI command:\n%s", want, joined)
		}
	}
}
