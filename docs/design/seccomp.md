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
- Solo5-hvt, 'urunc' does not have to do anything more, since solo5-hvt makes use
  of seccomp by itself, as of Solo5 v0.11.0.
- Solo5-spt, 'urunc' can not do anything since solo5-spt makes use of seccomp by
  itself.

## Caveats of using seccomp in 'urunc'

Since 'urunc', in most cases, makes use of the VMM's mechanisms to enforce the
seccomp filters, 'urunc' heavily relies on the VMM to properly restrict the system
calls the VMM can use.

Up to Solo5 v0.10.x, 'urunc' was the one applying the seccomp filters for
'Solo5-hvt' and therefore proper identification of the required system calls was
necessary. Unfortunately, due to dynamic linking and Go's runtime, it is
impossible to always predict correctly for every system the necessary system
calls for 'Solo5-hvt' execution. Since Solo5 v0.11.0, 'Solo5-hvt' makes use of
seccomp by itself and hence 'urunc' does not have to do anything more.

## Setting a seccomp profile

Due to its design, 'urunc' does not allow the definition of a seccomp profile other
than the default. However, users can totally disable seccomp by using
the `--security-opt seccomp=unconfined` command line option. In that scenario,
'urunc' will not make use of any seccomp filters in all the supported VMMs, except
of 'Solo5-hvt' and 'Solo5-spt', which make use of seccomp by themselves.
