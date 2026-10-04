# Running FreeBSD workloads in urunc

While FreeBSD is not a unikernel framework, it can boot as a lightweight guest
and run a single application. This guide walks through how to deploy FreeBSD
workloads on top of `urunc` as a FreeBSD microVM on
[Firecracker](https://github.com/firecracker-microvm/firecracker) or
[QEMU](https://qemu.org), configured (network, environment, user, command) by
[urunit](https://github.com/nubificus/urunit) as its init process.

Overall, we need to do the following:

1. Build or reuse a FreeBSD kernel.
2. (Optional but recommended) Include the
   [urunit](https://github.com/nubificus/urunit) init process.
3. Prepare the final image by appending the FreeBSD kernel (and `urunit`) and
   set up the `urunc` annotations.

## FreeBSD kernel

The main requirement for running FreeBSD workloads on top of `urunc` is a
FreeBSD kernel that boots directly (PVH) with virtio-mmio devices and ACPI. The
upstream `FIRECRACKER` kernel configuration is a good start, with
`hint.acpi.0.disabled="0"` (Firecracker >= 1.12 provides ACPI tables and no
longer a valid MP table) and `options EXT2FS` to utilize devmapper as a
snapshotter.  The kernel must include `virtio_mmio`, `virtio_blk` and `vtnet`;
for a 9p rootfs (see below) it must also include the `p9fs`/`virtio_p9fs`
client.

## Init process

The FreeBSD kernel starts `init` without arguments and does not pass boot
parameters as environment variables. Therefore, `urunc` passes the workload
information into the microVM using a raw virtio-block device. This is done
through the `urunit` configuration, which contains info about the network setup,
the application to execute, the working directory, uid/gid, etc.

`urunit` performs the following actions:

1. Opens `/dev/console` and reads the configuration from `/dev/vtbdN`.
2. Configures `vtnet0` with the IP, netmask and gateway of the container (a
   host route to the gateway is added first when it is outside the subnet).
3. Mounts block volumes (`/dev/vtbdN` in attach order, or
   `/dev/diskid/DISK-<serial>` with QEMU) as `ext2fs` or `ufs`.
4. Sets the environment variables, uid/gid and working directory of the
   container and executes the container command (`CMD`/`docker run` arguments).
5. Reaps children and reboots the VM when the application exits, which
   terminates the microVM.

If the image does not contain `urunit`, the kernel falls back to `/sbin/init`
of the image and the configuration is ignored.

The configuration format is the one described in the
[Linux tutorial](./existing-container-linux#init-process) with two additions:

```
UCS
UID:<uid>
GID:<gid>
WD:<working directory>
ARC:<number of arguments>
ARV:<argument>              (one line per argument)
UCE
...
UNS
IP:<ipv4 address>
GW:<gateway>
MSK:<netmask>
UNE
```

## Preparing the image

To differentiate traditional containers from unikernels, `urunc` uses specific
[annotations](../image-building#annotations). Therefore, to run a container with
a FreeBSD kernel on `urunc`, these annotations must be configured and the kernel
(along with `urunit`) must be included in the container image.

Since we are booting a full FreeBSD microVM, a root filesystem must be provided.
There are two ways to do this:

1. Using a block image (UFS2) as the rootfs, on Firecracker or QEMU.
2. Using the container's own rootfs directly, shared over 9p (QEMU only).

### Using a block image

#### Preparing the container image.

First create a block image with the root filesystem (UFS2 created with
`makefs(8)`) containing the application and `urunit` at `/urunit`.

To package everything as an OCI image we will use
[bunny](https://github.com/nubificus/bunny). There are two file formats we can
use:

a) bunnyfile:

```yaml
#syntax=harbor.nbfc.io/nubificus/bunny:latest
version: v0.1

platforms:
  framework: freebsd
  monitor: firecracker         #change to qemu for Qemu
  architecture: x86

rootfs:
  from: local
  path: rootfs.ufs
  type: block

kernel:
  from: local
  path: kernel

entrypoint: ["/urunit"]
cmd: ["/bin/sh"]
```

b) Dockerfile-like:

```Dockerfile
#syntax=harbor.nbfc.io/nubificus/bunny:latest

FROM scratch
COPY kernel /kernel
COPY rootfs.ufs /rootfs.ufs

LABEL "com.urunc.unikernel.binary"="/kernel"
LABEL "com.urunc.unikernel.block"="/rootfs.ufs"
LABEL "com.urunc.unikernel.blkMntPoint"="/"
LABEL "com.urunc.unikernel.unikernelType"="freebsd"
LABEL "com.urunc.unikernel.hypervisor"="firecracker" # change to qemu for Qemu

ENTRYPOINT ["/urunit"]
CMD ["/bin/sh"]
```

In both cases we can build the OCI image with the same command:

```bash
docker build -f bunnyfile -t freebsd/firecracker/block:latest .
```

#### Running the container

```bash
docker run --rm -e FOO=bar -w /tmp --runtime io.containerd.urunc.v2 freebsd/firecracker/block:latest
```

The guest gets the IP of the container, so services listening inside the microVM
are reachable at the container address, e.g. with `nc -l 8080` in the guest
and `curl http://<container-ip>:8080` on the host. `docker run --user
1001:1001` runs both the monitor and the application as that user.

### Using the container's rootfs directly

#### Preparing the container image.

There is also the option to use the container's own filesystem as the guest
root, without a block image, either using 9pfs or devmapper as a snapshotter.
In that case the image is a regular FreeBSD tree plus the kernel and `urunit`:

```yaml
#syntax=harbor.nbfc.io/nubificus/bunny:latest
version: v0.1

platforms:
  framework: freebsd
  monitor: qemu         #change to firecracker for Firecracker
  architecture: x86

rootfs:
  from: freebsd/freebsd-runtime:14.4
  type: raw
  include:
  - urunit:/urunit

kernel:
  from: local
  path: kernel

entrypoint: ["/urunit"]
cmd: ["/bin/sh"]
```

```Dockerfile
#syntax=harbor.nbfc.io/nubificus/bunny:latest
FROM --platform=freebsd/amd64 freebsd/freebsd-runtime:14.4
COPY kernel /kernel
COPY urunit /urunit

LABEL com.urunc.unikernel.binary=/kernel
LABEL com.urunc.unikernel.unikernelType=freebsd
LABEL com.urunc.unikernel.hypervisor=qemu       # Change to firecracker for Firecracker
LABEL com.urunc.unikernel.mountRootfs=true

ENTRYPOINT ["/urunit"]
CMD ["/bin/sh"]
```

In both cases we build the image with:

```bash
docker build -f bunnyfile -t freebsd/qemu/raw:latest .
```

#### Running the container

To run the image using 9pfs for the rootfs (QEMU only):

```bash
docker run --rm --runtime io.containerd.urunc.v2 freebsd/qemu/raw:latest
```

and with devmapper as a snapshotter for a block-based rootfs:

```bash
nerdctl run --rm --runtime io.containerd.urunc.v2 --snapshotter devmapper freebsd/qemu/raw:latest
```

> NOTE: Docker requires special configuration to use devmapper as a snapshotter.

## Limitations

- `/etc/resolv.conf` and `/etc/hosts` of the container are not visible in the
  guest when the rootfs is a block image inside the container image (they are
  with a 9p rootfs).
