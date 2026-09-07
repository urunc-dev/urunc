//go:build darwin

package hypervisors

import (
	"fmt"
	"os"
	"strconv"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	HviVmm    VmmType = "hvi"
	HviBinary string  = "hvi"
)

// HviDarwin runs an arm64 Linux guest with Hypervisor.framework and HVI's
// in-process virtio devices. Generic container boot exports the unpacked OCI
// directory read-only through HVI's virtio-fs backend; the initrd supplies the
// writable overlay. Additional tagged directories retain their requested
// read-only/read-write mode.
type HviDarwin struct {
	binaryPath string
}

func NewHviDarwin(binaryPath string) *HviDarwin {
	return &HviDarwin{binaryPath: binaryPath}
}

func (h *HviDarwin) BuildExecCmd(args types.ExecArgs, _ types.Unikernel) ([]string, error) {
	kernel := args.KernelPath
	if kernel == "" {
		kernel = args.UnikernelPath
	}
	if kernel == "" {
		return nil, fmt.Errorf("hvi: no kernel image to boot")
	}
	mem := DefaultMemory
	if args.MemSizeB != 0 {
		mem = bytesToMiB(args.MemSizeB)
	}
	vcpus := args.VCPUs
	if vcpus == 0 {
		vcpus = 1
	}
	cmd := []string{
		h.Path(), "boot",
		"--kernel", kernel,
		"--mem-mib", strconv.FormatUint(mem, 10),
		"--cpus", strconv.FormatUint(uint64(vcpus), 10),
	}
	// Tell the file server who the workload is, so the shared directories
	// come back owned by it. Only when it is not root: root is hvi's default,
	// and not passing the flags keeps this working against an hvi that
	// predates them.
	if args.GuestUID != 0 || args.GuestGID != 0 {
		cmd = append(cmd,
			"--fs-uid", strconv.FormatUint(uint64(args.GuestUID), 10),
			"--fs-gid", strconv.FormatUint(uint64(args.GuestGID), 10))
	}
	if args.InitrdPath != "" {
		cmd = append(cmd, "--initramfs", args.InitrdPath)
	}
	if args.Command != "" {
		cmd = append(cmd, "--cmdline", args.Command)
	}
	if args.BlockDevPath != "" {
		cmd = append(cmd, "--disk", args.BlockDevPath)
	}
	seenTags := make(map[string]bool)
	appendShare := func(path, tag string, readOnly bool) error {
		if path == "" || tag == "" {
			return fmt.Errorf("hvi virtio-fs exports require a path and tag")
		}
		if seenTags[tag] {
			return fmt.Errorf("duplicate hvi virtio-fs tag %q", tag)
		}
		seenTags[tag] = true
		flag := "--share-rw"
		if readOnly {
			flag = "--share-ro"
		}
		cmd = append(cmd, flag, path, tag)
		return nil
	}
	if args.Sharedfs.Path != "" {
		tag := args.Sharedfs.Tag
		if tag == "" {
			tag = "rootfs"
		}
		if err := appendShare(args.Sharedfs.Path, tag, args.Sharedfs.ReadOnly); err != nil {
			return nil, err
		}
	}
	for _, dir := range args.SharedDirs {
		if err := appendShare(dir.Path, dir.Tag, dir.ReadOnly); err != nil {
			return nil, err
		}
	}
	if args.Net.UnixSocket != "" {
		cmd = append(cmd, "--net-gateway", args.Net.UnixSocket)
	} else if args.Net.TapDev != "" {
		cmd = append(cmd, "--net")
	}
	if args.AgentSockPath != "" {
		cmd = append(cmd, "--agent-sock", args.AgentSockPath)
	}
	if args.ContainerID != "" {
		cmd = append(cmd, "--sandbox-id", args.ContainerID)
	}
	return cmd, nil
}

func (h *HviDarwin) PreExec(_ types.ExecArgs) error { return nil }

func (h *HviDarwin) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

func (h *HviDarwin) Stop(pid int) error { return killProcess(pid) }

func (h *HviDarwin) Path() string { return h.binaryPath }

func (h *HviDarwin) UsesKVM() bool { return false }

func (h *HviDarwin) SupportsSharedfs(fsType string) bool { return fsType == "virtiofs" }

func (h *HviDarwin) Ok() error {
	info, err := os.Stat(h.binaryPath)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("hvi not found or not executable at %s", h.binaryPath)
	}
	return nil
}
