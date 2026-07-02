---
layout: default
title: "Seccomp"
description: "Seccomp in urunc"
---

# Seccomp in Urunc

## Overview

Seccomp (Secure Computing Mode) is a Linux kernel security feature that
restricts the system calls a process can make, limiting the kernel exposure
to the processes. Container runtimes make use of this mechanism to
further limit a container and enhance overall security. 

## How Seccomp is used in 'urunc'

In 'urunc' the application does not execute directly in the host kernel. Instead,
'urunc' makes use of either a VMM (Virtual Machine Monitor) or the `solo5-spt`
tender to execute the application inside a unikernel. As a result, in contrast
with other container runtimes, in 'urunc' the applications do not share the same
kernel.

Thus, a malicious user must take control of the guest kernel and escape to the
VMM before attacking the host. To further limit the exposure of
the host kernel to the VMM, 'urunc' uses seccomp filters for each
supported VMM. In particular, in the case of:
- Firecracker, 'urunc' does not have to do anything more, since Firecracker by
  default makes use of seccomp filters.
- Qemu, 'urunc' makes use of Qemu's sandbox command line options to activate
  all possible seccomp filters in Qemu.
- Cloud-Hypervisor, 'urunc' makes use of the `--seccomp true` command line
  options to enable Cloud-Hypervisor's seccomp filters.
- Solo5-hvt, 'urunc' applies the seccomp filters before executing
  'Solo5-hvt'.
- Solo5-spt, 'urunc' can not do anything since solo5-spt makes use of seccomp by
  itself.

## Caveats of using seccomp in 'urunc'

Since 'urunc', in most cases, makes use of the VMM's mechanisms to enforce the
seccomp filters, 'urunc' heavily relies on the VMM to properly restrict the system
calls the VMM can use.

In the case of 'Solo5-hvt', since 'urunc' is responsible for applying the seccomp
filters, proper identification of the required system calls is necessary.
Unfortunately, due to dynamic linking and Go's runtime, it is
impossible to always predict correctly for every system the necessary system
calls for 'Solo5-hvt' execution.

Nevertheless, 'Solo5-hvt' with seccomp in 'urunc' has been tested in Ubuntu 20.04
and Ubuntu 22.04. Using 'urunc' and solo5-hvt on different platforms might result
in failed execution. For that reason, we strongly recommend running the seccomp
test first, by `make test_nerdctl_Seccomp`. In case the test fails, the seccomp
profile for 'Solo5-hvt' needs to get updated.

For that reason, we created a toolset to identify the required system calls.
The toolset, along with instructions on how to use it, can be found in [goscall
repository](https://github.com/nubificus/goscall).

## Setting a seccomp profile

Due to its design, 'urunc' does not allow the definition of a seccomp profile other
than the default. However, users can totally disable seccomp by using
the `--security-opt seccomp=unconfined` command line option. In that scenario,
'urunc' will not make use of any seccomp filters in all the supported VMMs, except
of 'Solo5-spt'.
