package hypervisors

import (
	"runtime"
	"testing"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestQemuUsesKVM(t *testing.T) {
	q := &Qemu{}

	if !q.UsesKVM() {
		t.Fatal("expected UsesKVM() to return true")
	}
}

func TestQemuSupportsSharedfs(t *testing.T) {
	q := &Qemu{}

	if !q.SupportsSharedfs("9pfs") {
		t.Fatal("expected SupportsSharedfs() to return true")
	}
}

func TestQemuPath(t *testing.T) {
	q := &Qemu{
		binaryPath: "/usr/bin/qemu-system-x86_64",
	}

	if q.Path() != "/usr/bin/qemu-system-x86_64" {
		t.Fatal("unexpected path")
	}
}

func TestQemuPreExec(t *testing.T) {
	q := &Qemu{}

	err := q.PreExec(types.ExecArgs{})

	if err != nil {
		t.Fatalf("expected nil error got %v", err)
	}
}

func TestGetVirtioNetArg(t *testing.T) {
	got := getVirtioNetArg()

	expected := "-device virtio-net-pci,netdev=net0"

	if runtime.GOARCH == "arm64" {
		expected = "-device virtio-net-device,netdev=net0"
	}

	if got != expected {
		t.Fatalf(
			"expected %s got %s",
			expected,
			got,
		)
	}
}

type mockUnikernel struct{}

func (m mockUnikernel) Init(types.UnikernelParams) error {
	return nil
}

func (m mockUnikernel) CommandString() (string, error) {
	return "", nil
}

func (m mockUnikernel) SupportsBlock() bool {
	return false
}

func (m mockUnikernel) SupportsFS(string) bool {
	return false
}

func (m mockUnikernel) MonitorNetCli(string, string) string {
	return ""
}

func (m mockUnikernel) MonitorBlockCli() []types.MonitorBlockArgs {
	return nil
}

func (m mockUnikernel) MonitorCli() types.MonitorCliArgs {
	return types.MonitorCliArgs{}
}

func TestQemuBuildExecCmd(t *testing.T) {
	q := &Qemu{
		binaryPath: "/usr/bin/qemu-system-x86_64",
	}

	args := types.ExecArgs{
		MemSizeB:      512 * 1024 * 1024,
		VCPUs:         2,
		UnikernelPath: "/tmp/kernel",
		Command:       "console=ttyS0",
	}

	cmd, err := q.BuildExecCmd(
		args,
		mockUnikernel{},
	)

	if err != nil {
		t.Fatalf(
			"unexpected error: %v",
			err,
		)
	}

	if len(cmd) == 0 {
		t.Fatal(
			"expected non-empty command",
		)
	}

	if cmd[0] != "/usr/bin/qemu-system-x86_64" {
		t.Fatalf(
			"unexpected binary: %s",
			cmd[0],
		)
	}
}
