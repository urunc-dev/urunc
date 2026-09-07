//go:build darwin
// +build darwin

package hypervisors

import (
	"strings"
	"testing"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// The darwin-configured Qemu (same type, platform fields set) must emit the
// HVF accelerator, the Homebrew firmware path, and a stream netdev for the
// user-mode gateway — never the KVM/tap Linux flags.
func TestNewQemuDarwinCommand(t *testing.T) {
	q := NewQemuDarwin("/opt/homebrew/bin/qemu-system-aarch64")
	if q.UsesKVM() {
		t.Error("darwin backend must not report KVM")
	}
	args := types.ExecArgs{
		UnikernelPath: "/img/kernel",
		KernelPath:    "/img/kernel",
		Command:       "console=hvc0",
		Net:           types.NetDevParams{UnixSocket: "/tmp/gw.sock", MAC: "52:54:00:aa:bb:cc"},
	}
	out, err := q.BuildExecCmd(args, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	for _, want := range []string{"-accel hvf", "-L /opt/homebrew/share/qemu", "-netdev stream", "-qmp unix:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in darwin command:\n%s", want, joined)
		}
	}
	for _, notWant := range []string{"-enable-kvm", "-netdev tap"} {
		if strings.Contains(joined, notWant) {
			t.Errorf("unexpected Linux flag %q in darwin command", notWant)
		}
	}
}
