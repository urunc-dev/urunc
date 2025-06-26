// Copyright (c) 2023-2025, Nubificus LTD
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

package hypervisors

import (
	"os/exec"
	"strings"
	"syscall"

	seccomp "github.com/elastic/go-seccomp-bpf"
	"github.com/nubificus/urunc/pkg/unikontainers/unikernels"
)

const (
	HvtVmm    VmmType = "hvt"
	HvtBinary string  = "solo5-hvt"
)

type HVT struct {
	binaryPath string
	binary     string
}

// applySeccompFilter applies some secomp filters for the Hvt process.
// By default all systemcalls will cause a SIGSYS, except the ones that we whitelist
func applySeccompFilter() error {
	syscalls := []string{
		"rt_sigaction",
		"ioctl",
		"pread64",
		"mmap",
		"recvmsg",
		"openat",
		"sendto",
		"mprotect",
		"write",
		"epoll_ctl",
		"epoll_create1",
		"read",
		"open",
		"close",
		"fstat",
		"stat",
		"munmap",
		"brk",
		"access",
		"execve",
		"timerfd_create",
		"arch_prctl",
		"lseek",
		"personality",
		"socket",
		"bind",
		"getsockname",
		"exit",
		"exit_group",
		"getpid",
		"tgkill",
		"nanosleep",
		"futex",
		"epoll_pwait",
		"rt_sigreturn",
		"timerfd_settime",
		"pwrite64",
		"newfstatat",
		"set_tid_address",
		"set_robust_list",
		"rseq",
		"prlimit64",
		"getrandom",
	}
	// Some of the actions that we can take for accessing non-permitted system calls are:
	// - seccomp.ActionKillThread will kill the thread that tried to use a non-permitted
	//	system call, but the rest of the threads can still run
	// - seccomp.ActionErrno will result to returning EPERM error in all non-permitted
	//	system calls.
	// - ActionTrap will cause a SIGSYS trap to the process.
	//
	// For the time being, we choose ActionTrap, but we can change this in the future.
	filter := seccomp.Filter{
		// Set the threads no_new_privs bit, disabling any new child or execve
		// system call to grant privileges that the parent does not have.
		NoNewPrivs: true,
		// Sync the filter to all threads created by the Go runtime.
		Flag: seccomp.FilterFlagTSync,
		Policy: seccomp.Policy{
			DefaultAction: seccomp.ActionTrap,
			Syscalls: []seccomp.SyscallGroup{
				{
					Action: seccomp.ActionAllow,
					Names:  syscalls,
				},
			},
		},
	}

	err := seccomp.LoadFilter(filter)
	if err != nil {
		vmmLog.Error("Could not load seccomp filters")
		return err
	}

	vmmLog.Debug("Loaded seccomp filters")
	vmmLog.WithField("allowed syscalls", syscalls).Debug("Whitelisted system calls")

	return nil
}

// Stop is an empty function to satisfy VMM interface compatibility requirements.
// It does not perform any actions and always returns nil.
func (h *HVT) Stop(_ string) error {
	return nil
}

// UsesKVM returns a bool value depending on if the monitor uses KVM
func (h *HVT) UsesKVM() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (h *HVT) SupportsSharedfs() bool {
	return false
}

// Path returns the path to the hvt binary.
func (h *HVT) Path() string {
	return h.binaryPath
}

// Ok checks if the hvt binary is available in the system's PATH.
func (h *HVT) Ok() error {
	if _, err := exec.LookPath(HvtBinary); err != nil {
		return ErrVMMNotInstalled
	}
	return nil
}

func (h *HVT) Execve(args ExecArgs, ukernel unikernels.Unikernel) error {
	hvtString := string(HvtVmm)
	hvtMem := bytesToStringMB(args.MemSizeB)
	cmdString := h.binaryPath + " --mem=" + hvtMem
	cmdString = appendNonEmpty(cmdString, " "+ukernel.MonitorNetCli(hvtString), args.TapDevice)
	cmdString = appendNonEmpty(cmdString, " "+ukernel.MonitorBlockCli(hvtString), args.BlockDevice)
	cmdString = appendNonEmpty(cmdString, " ", ukernel.MonitorCli(hvtString))
	cmdString += " " + args.UnikernelPath + " " + args.Command
	cmdArgs := strings.Split(cmdString, " ")
	if args.Seccomp {
		err := applySeccompFilter()
		if err != nil {
			return err
		}
	}
	vmmLog.WithField("hvt command", cmdString).Debug("Ready to execve hvt")
	return syscall.Exec(h.binaryPath, cmdArgs, args.Environment) //nolint: gosec
}
