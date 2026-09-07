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
