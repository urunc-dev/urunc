//go:build darwin

package hypervisors

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const VzVmm VmmType = "vz"

// VzDarwin implements the VMM interface for Apple Virtualization.framework
type VzDarwin struct {
	vzRunnerPath string
	qmpSocket    string
}

// NewVzDarwin creates a new Vz VMM instance
func NewVzDarwin() *VzDarwin {
	// Find vz-runner in the same directory as urunc-macos
	exePath, err := os.Executable()
	if err != nil {
		exePath = "./vz-runner"
	} else {
		exePath = filepath.Join(filepath.Dir(exePath), "vz-runner")
	}

	return &VzDarwin{
		vzRunnerPath: exePath,
	}
}

// BuildExecCmd builds the command to launch the VM via vz-runner
func (v *VzDarwin) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	if args.KernelPath == "" {
		return nil, fmt.Errorf("kernel path required for Vz backend")
	}

	cmdArgs := []string{
		v.vzRunnerPath,
		"--kernel", args.KernelPath,
		"--mem", fmt.Sprintf("%d", args.MemSizeB/(1024*1024)),
		"--cpus", fmt.Sprintf("%d", args.VCPUs),
	}

	// Add initrd if provided
	if args.InitrdPath != "" {
		cmdArgs = append(cmdArgs, "--initrd", args.InitrdPath)
	}

	// Add kernel cmdline
	if args.Command != "" {
		cmdArgs = append(cmdArgs, "--cmdline", args.Command)
	}

	// Deterministic MAC address for the NAT network device, so the guest's
	// DHCP lease (and therefore its IP) can be found on the host by MAC.
	if args.Net.MAC != "" {
		cmdArgs = append(cmdArgs, "--mac", args.Net.MAC)
	}

	// Block device: attach ext4 image as virtio-blk disk
	if args.BlockDevPath != "" {
		cmdArgs = append(cmdArgs, "--rootfs", args.BlockDevPath)
	} else if args.Sharedfs.Path != "" {
		// Root filesystem over virtiofs. Mount tag fs0 matches the shared Linux
		// unikernel builder's cmdline (root=fs0 rootfstype=virtiofs), the same
		// convention the QEMU backend uses, so both monitors boot the identical
		// kernel command line.
		cmdArgs = append(cmdArgs, "--share", args.Sharedfs.Path, "fs0")
	} else if args.RootfsPath != "" && args.InitrdPath == "" {
		// Legacy root share: a caller that builds its own kernel command line
		// (e.g. the macOS product with root=rootfs) passes RootfsPath instead
		// of Sharedfs; share it under mount tag "rootfs" to match, mirroring
		// the QEMU backend's darwinRootfsArgs.
		info, err := os.Stat(args.RootfsPath)
		if err == nil && info.IsDir() {
			cmdArgs = append(cmdArgs, "--share", args.RootfsPath, "rootfs")
		}
	}

	// Additional tagged shares (one --share per directory)
	for _, dir := range args.SharedDirs {
		cmdArgs = append(cmdArgs, "--share", dir.Path, dir.Tag)
	}

	// QMP socket for graceful shutdown control
	if v.qmpSocket != "" {
		cmdArgs = append(cmdArgs, "--qmp", v.qmpSocket)
	}

	// Agent transport: vz-runner bridges this host unix socket to guest
	// vsock port 1024, where urunit-agent serves exec sessions.
	if args.AgentSockPath != "" {
		cmdArgs = append(cmdArgs, "--agent-sock", args.AgentSockPath)
	}

	return cmdArgs, nil
}

// PreExec performs any pre-execution setup
func (v *VzDarwin) PreExec(args types.ExecArgs) error {
	// Create QMP socket directory if needed
	if v.qmpSocket != "" {
		sockDir := filepath.Dir(v.qmpSocket)
		if err := os.MkdirAll(sockDir, 0700); err != nil {
			return fmt.Errorf("failed to create QMP socket directory: %w", err)
		}
	}
	return nil
}

// Signal sends a signal to the vz-runner process
func (v *VzDarwin) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

// Stop stops the VM
func (v *VzDarwin) Stop(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	// Try graceful shutdown via SIGTERM first
	// The vz-runner process has signal handlers that will gracefully stop the VM
	if err := proc.Signal(os.Interrupt); err != nil {
		// If SIGTERM fails, fall back to SIGKILL
		return proc.Kill()
	}

	// Give the process time to shut down gracefully (5 seconds)
	// This is a simplified timeout; could be enhanced with polling
	done := make(chan error, 1)
	go func() {
		_, err := proc.Wait()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil && err.Error() != "waitid: no child processes" {
			return err
		}
		return nil
	case <-time.After(5 * time.Second):
		// If still running after 5 seconds, force kill
		return proc.Kill()
	}
}

// Path returns the path to the vz-runner binary
func (v *VzDarwin) Path() string {
	return v.vzRunnerPath
}

// UsesKVM returns false (Vz uses HVF, not KVM)
func (v *VzDarwin) UsesKVM() bool {
	return false
}

// SupportsSharedfs returns true (Vz supports directory sharing)
func (v *VzDarwin) SupportsSharedfs(_ string) bool {
	return true
}

// Ok checks if the Vz backend is available
func (v *VzDarwin) Ok() error {
	// Check if vz-runner exists and is executable
	info, err := os.Stat(v.vzRunnerPath)
	if err != nil {
		return fmt.Errorf("vz-runner not found at %s: %w", v.vzRunnerPath, err)
	}

	if info.IsDir() {
		return fmt.Errorf("vz-runner is a directory, not an executable")
	}

	return nil
}

// SetQMPSocket sets the QMP socket path for this instance
func (v *VzDarwin) SetQMPSocket(path string) {
	v.qmpSocket = path
}
