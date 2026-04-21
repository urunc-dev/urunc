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

//go:build linux

package unikontainers

import "golang.org/x/sys/unix"

// enableProcessKSM sets MMF_VM_MERGE_ANY on the current mm via
// prctl(PR_SET_MEMORY_MERGE, 1) so every future anonymous mmap is
// auto-marked MADV_MERGEABLE. The kernel's ksmd (gated by
// /sys/kernel/mm/ksm/run=1 on the host) then dedups identical pages
// across processes that have the bit set.
//
// MMF_VM_MERGE_ANY is in MMF_INIT_MASK, so the flag survives the
// subsequent syscall.Exec into the VMM — firecracker/qemu/etc. don't
// need to opt in themselves, and they don't need to call madvise on
// their guest RAM. This matters for firecracker in particular, whose
// seccomp filter only allows madvise(DONTNEED).
//
// Best-effort. prctl returns EINVAL on kernels older than 6.4 or
// without CONFIG_KSM=y; we log and continue — the VMM still boots, it
// just won't benefit from KSM.
func enableProcessKSM() {
	if err := unix.Prctl(unix.PR_SET_MEMORY_MERGE, 1, 0, 0, 0); err != nil {
		uniklog.WithError(err).Warn("PR_SET_MEMORY_MERGE unsupported; skipping KSM opt-in for this VMM")
		return
	}
	uniklog.Debug("PR_SET_MEMORY_MERGE enabled; VMM guest memory will be KSM-eligible after execve")
}
