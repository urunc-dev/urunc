//go:build darwin
// +build darwin

package hypervisors

import (
	"strings"
	"testing"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// A read-only tagged share must go out over --share-ro, and a read-write one
// over --share. The flag carries the mode rather than a third positional, so
// the argument shape stays fixed; getting this wrong would hand the guest a
// writable mount of a directory the caller asked to protect.
func TestVzDarwinSharedDirReadOnly(t *testing.T) {
	args := types.ExecArgs{
		UnikernelPath: "/img/kernel",
		KernelPath:    "/img/kernel",
		Command:       "console=hvc0",
		SharedDirs: []types.SharedDirParams{
			{Path: "/host/rw", Tag: "rwtag"},
			{Path: "/host/ro", Tag: "rotag", ReadOnly: true},
		},
	}
	out, err := NewVzDarwin().BuildExecCmd(args, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	if !strings.Contains(joined, "--share /host/rw rwtag") {
		t.Errorf("read-write share should use --share:\n%s", joined)
	}
	if !strings.Contains(joined, "--share-ro /host/ro rotag") {
		t.Errorf("read-only share should use --share-ro:\n%s", joined)
	}
	// The dangerous regression: a read-only share silently emitted as --share.
	if strings.Contains(joined, "--share /host/ro") {
		t.Errorf("read-only share was emitted read-write:\n%s", joined)
	}
}

// The root shares are what the guest boots from and must stay writable
// whatever the tagged shares ask for.
func TestVzDarwinRootShareStaysWritable(t *testing.T) {
	args := types.ExecArgs{
		UnikernelPath: "/img/kernel",
		KernelPath:    "/img/kernel",
		Command:       "console=hvc0",
		Sharedfs:      types.SharedfsParams{Path: "/host/root"},
		SharedDirs:    []types.SharedDirParams{{Path: "/host/ro", Tag: "rotag", ReadOnly: true}},
	}
	out, err := NewVzDarwin().BuildExecCmd(args, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	if !strings.Contains(joined, "--share /host/root fs0") {
		t.Errorf("root share must stay read-write:\n%s", joined)
	}
}
