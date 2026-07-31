---
layout: default
title: "Storage handling"
description: "How urunc prepares and attaches guest storage"
---

# Storage handling in urunc

This page describes selected storage behaviors that are easy to miss when
reading only the packaging guides. For how to build and package a guest rootfs
(initrd, block, or shared-fs), see [Creating rootfs for unikernels](../package/rootfs.md).

## Bind mounts attached as guest block devices

Some guests can consume host bind mounts as virtio-block devices when the
mounted filesystem type is supported by the unikernel. During create, `urunc`
inspects the container mounts, and for each eligible bind mount it:

1. Resolves the mount source from `/proc/self/mountinfo`.
2. Detaches the host mount so the backing device can be handed to the guest.
3. Attaches that source as a block device in the sandbox.

### Loop devices and the autoclear flag

When the bind mount source is a regular file (for example an ext2 image), the
kernel typically exposes it through a [loop
device](https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-block-loop).
`/proc/self/mountinfo` then lists that loop device (for example `/dev/loopN`)
as the mount source.

If `urunc` unmounted the filesystem while the loop device still had
**autoclear** enabled, the kernel could destroy the loop device as soon as the
last mount was gone. In that case there would be nothing left to attach to the
guest.

To avoid that, `urunc` clears the loop device autoclear flag before unmounting
the host mount. The loop device therefore remains available for attachment to
the sandbox.

### Restore on delete

On the delete path, `urunc` remounts the original host mountpoint when needed
and, if autoclear was cleared during create, restores the autoclear flag on the
loop device.

> **Note:** If the container is never deleted through `urunc` (for example the
> process is killed and state is discarded without a delete), the autoclear
> flag may stay cleared and the host mount may not be remounted. Operators
> should ensure container delete runs for workloads that use this path, or
> clean up leftover loop devices manually if create/delete is interrupted.

### Where this lives in the code

- Clearing autoclear and unmounting eligible bind mounts: `getBlockVolumes` in
  `pkg/unikontainers/block.go`
- Remounting and restoring autoclear: `restoreBlockVolumes` in the same file,
  called from the unikontainer delete path
