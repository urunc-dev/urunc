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

// Legacy root shares remain writable by default. Generic container boot opts
// into a read-only lower layer explicitly in the next test.
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

func TestVzDarwinContainerBootRootShareIsTaggedReadOnly(t *testing.T) {
	args := types.ExecArgs{
		UnikernelPath: "/host/Image",
		KernelPath:    "/host/Image",
		InitrdPath:    "/instance/container-initrd",
		Command:       "rdinit=/vz-init console=hvc0",
		Sharedfs: types.SharedfsParams{
			Path: "/store/ubuntu/rootfs", Tag: "rootfs", ReadOnly: true,
		},
	}
	out, err := NewVzDarwin().BuildExecCmd(args, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	for _, want := range []string{
		"--kernel /host/Image",
		"--initrd /instance/container-initrd",
		"--share-ro /store/ubuntu/rootfs rootfs",
		"--cmdline rdinit=/vz-init console=hvc0",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in Vz command:\n%s", want, joined)
		}
	}
}

func TestVzDarwinDisks(t *testing.T) {
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
			name: "legacy BlockDevPath is the root disk",
			args: types.ExecArgs{BlockDevPath: "/instance/root.ext4"},
			want: []string{"--rootfs", "/instance/root.ext4"},
		},
		{
			// vz-runner numbers the serials over --disk and --disk-ro only,
			// so the order here is the order of disk0, disk1.
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.args.KernelPath = "/host/Image"
			argv, err := NewVzDarwin().BuildExecCmd(tc.args, &fakeUnikernel{})
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

func TestVzDarwinDisksRejected(t *testing.T) {
	cases := []struct {
		name  string
		disks []types.BlockDevSpec
		want  string
	}{
		{
			name:  "empty path",
			disks: []types.BlockDevSpec{{Path: "/store/lower.ext4", ReadOnly: true}, {}},
			want:  "vz: block device 1 has no path",
		},
		{
			name: "duplicate path",
			disks: []types.BlockDevSpec{
				{Path: "/instance/disk.ext4", ReadOnly: true},
				{Path: "/instance/disk.ext4"},
			},
			want: `vz: duplicate block device "/instance/disk.ext4"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewVzDarwin().BuildExecCmd(types.ExecArgs{
				KernelPath: "/host/Image",
				BlockDevs:  tc.disks,
			}, &fakeUnikernel{})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got error %v, want %q", err, tc.want)
			}
		})
	}
}

// Disks in BlockDevs leave the RootfsPath share in place, as they do the
// Sharedfs root share.
func TestVzDarwinDisksKeepRootfsPathShare(t *testing.T) {
	dir := t.TempDir()
	out, err := NewVzDarwin().BuildExecCmd(types.ExecArgs{
		KernelPath: "/host/Image",
		RootfsPath: dir,
		BlockDevs:  []types.BlockDevSpec{{Path: "/instance/upper.ext4"}},
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	for _, want := range []string{
		"--share " + dir + " rootfs",
		"--disk /instance/upper.ext4",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in Vz command:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "--rootfs") {
		t.Errorf("BlockDevs must not emit --rootfs:\n%s", joined)
	}
}

// The legacy root disk replaces the root share. Disks in BlockDevs do not:
// an overlay boot needs both the share and the disks on the command line.
func TestVzDarwinDisksKeepRootShare(t *testing.T) {
	share := types.SharedfsParams{Path: "/store/rootfs", ReadOnly: true}

	out, err := NewVzDarwin().BuildExecCmd(types.ExecArgs{
		KernelPath: "/host/Image",
		Sharedfs:   share,
		BlockDevs: []types.BlockDevSpec{
			{Path: "/store/lower.ext4", ReadOnly: true},
			{Path: "/instance/upper.ext4"},
		},
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	for _, want := range []string{
		"--share-ro /store/rootfs fs0",
		"--disk-ro /store/lower.ext4 --disk /instance/upper.ext4",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in Vz command:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "--rootfs") {
		t.Errorf("BlockDevs must not emit --rootfs:\n%s", joined)
	}

	out, err = NewVzDarwin().BuildExecCmd(types.ExecArgs{
		KernelPath:   "/host/Image",
		Sharedfs:     share,
		BlockDevPath: "/instance/root.ext4",
	}, &fakeUnikernel{})
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(out, " ")
	if !strings.Contains(joined, "--rootfs /instance/root.ext4") {
		t.Errorf("legacy root disk missing:\n%s", joined)
	}
	if strings.Contains(joined, "fs0") {
		t.Errorf("legacy root disk must replace the root share:\n%s", joined)
	}
}
