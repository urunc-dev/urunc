# Unikernel support

Unikernels are specialized, minimalistic operating systems constructed to run a
single application. By compiling only the necessary components of an OS into
the final image, unikernels offer improved performance, security, and smaller
footprints compared to traditional OS-based virtual machines.

One of the main goals of `urunc` is to bridge the gap between unikernels and
the cloud-native ecosystem. For that reason, `urunc` aims to support all the
available unikernel frameworks and similar technologies.

For the time being, `urunc` provides support for
[Unikraft](https://unikraft.org/) and
[Rumprun](https://github.com/cloudkernels/rumprun) unikernels.

## Unikraft

[Unikraft](https://unikraft.org/) is a POSIX-friendly and highly modular
unikernel framework designed to make it easier to build optimized, lightweight,
and high-performance unikernels. Unlike traditional monolithic unikernel
approaches, [Unikraft](https://unikraft.org/) allows developers to include only
the components necessary for their application, resulting in reduced footprint
and improved performance. At the same time, [Unikraft](https://unikraft.org/)
offers Linux binary compatibility allowing easier and effortless execution
of existing applications on top of [Unikraft](https://unikraft.org/).  With
support for various programming languages and environments,
[Unikraft](https://unikraft.org/) is ideal for building unikernels across a
wide range of use cases.

### VMMs and other sandbox monitors

[Unikraft](https://unikraft.org/) can boot on top of both Xen and KVM
hypervisors. Especially in the case of KVM, [Unikraft](https://unikraft.org/)
supports [Qemu](https://www.qemu.org/) and [AWS
Firecracker](https://github.com/firecracker-microvm/firecracker). In both
cases, it gets network access through virtio-net. In the case of storage, to
the best of our knowledge [Unikraft](https://unikraft.org/) supports two
options: a) 9pfs sharing a directory between the host and the unikernel and b)
initrd and therefore an initial RamFS.

### Unikraft and `urunc`

In the case of [Unikraft](https://unikraft.org/), `urunc` supports both network
and storage I/O over both [Qemu](https://qemu.org) and
[Firecracker](https://github.com/firecracker-microvm/firecracker) VMMs.
However, for the time being, `urunc` only offers support for the initrd option
of [Unikraft](https://unikraft.org/) and not for shared-fs. On the other hand,
the shared-fs option is Work-In-Progress and we will soon provide an update
about this.

[Unikraft](https://unikraft.org/) maintains a
[catalog](https://github.com/unikraft/catalog) with available applications as
unikernel images. Check out our [packaging](../package) page on how to
get these images and run them on top of `urunc`.

An example of [Unikraft](https://unikraft.org/) on top of
[Qemu](https://qemu.org) with `urunc`:

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/nginx-qemu-unikraft-initrd:latest
```

Another example of [Unikraft](https://unikraft.org/) on top of
[Firecracker](https://github.com/firecracker-microvm/firecracker) with `urunc`:

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/nginx-firecracker-unikraft-initrd:latest
```

## Mirage

[MirageOS](https://github.com/mirage/mirage) is a library operating system that
constructs unikernels for secure, high-performance network applications across
various cloud computing and mobile platforms.
[MirageOS](https://github.com/mirage/mirage) uses the OCaml language, with
libraries that provide networking, storage and concurrency support that work
under Unix during development, but become operating system drivers when being
compiled for production deployment. We can easily set up and build
[MirageOS](https://github.com/mirage/mirage) unikernels with `mirage`, which can
be installed through the [Opam](https://opam.ocaml.org/) source package manager.
The framework is fully event-driven, with no support for preemptive threading.

[MirageOS](https://github.com/mirage/mirage) is characterized from the extremely
fast start up times (just a few milliseconds), small binaries (usually a few
megabytes), small footprint (requires a few megabytes of memory) and safe logic,
as it is completely written in OCaml.

### VMMs and other sandbox monitors

[MirageOS](https://github.com/mirage/mirage), as one of the first unikernel
frameworks, provides support for a variety of hypervisors and platforms. In
particular, [MirageOS](https://github.com/mirage/mirage) makes use of
[Solo5](https://github.com/Solo5/solo5) and can execute as a VM over KVM/Xen
and other OSes, such as BSD OSes (FreeBSD, OpenBSD) or even Muen. Especially
for KVM, [MirageOS](https://github.com/mirage/mirage) supports
[Qemu](https://www.qemu.org/) and
[Solo5-hvt](https://github.com/Solo5/solo5).  It can access the network
through virtio-net in the case of [Qemu](https://qemu.org) and using
[Solo5](https://github.com/Solo5/solo5)'s I/O interface in the case of
[Solo5](https://github.com/Solo5/solo5). For storage,
[MirageOS](https://github.com/mirage/mirage) supports block-based storage
through virtio-block and [Solo5](https://github.com/Solo5/solo5)'s I/O in
[Qemu](https://qemu.org) and [Solo5](https://github.com/Solo5/solo5)
respectively.

Furthermore, [MirageOS](https://github.com/mirage/mirage) is also possible to
execute on top of [Solo5-spt](https://github.com/Solo5/solo5) a sandbox monitor
of [Solo5](https://github.com/Solo5/solo5) project that does not use
hardware-assisted virtualization. In that context,
[MirageOS](https://github.com/mirage/mirage) can access network and block
storage through [Solo5](https://github.com/Solo5/solo5)'s I/O interface.

### MirageOS and `urunc`

In the case of [MirageOS](https://github.com/mirage/mirage) `urunc` provides
support for [Solo5](https://github.com/Solo5/solo5),
[Solo5](https://github.com/Solo5/solo5) and [Qemu](https://qemu.org). For all
monitors of [Solo5](https://github.com/Solo5/solo5) `urunc` allows the access
of both network and block storage through
[Solo5](https://github.com/Solo5/solo5)'s I/O interface and for
[Qemu](https://qemu.org) through virtio-net and virtio-block.

For the time being, the block image that the
[MirageOS](https://github.com/mirage/mirage) unikernel access during its
execution should be placed inside the container image.

For more information on packaging
[MirageOS](https://github.com/mirage/mirage) unikernels for `urunc` take
a look at our [packaging](../package/) page.

An example of [MirageOS](https://github.com/mirage/mirage) on top of
[Solo5](https://github.com/Solo5/solo5) using a block image inside the
container's rootfs with 'urunc':

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/net-mirage-hvt:latest
```

An example of [MirageOS](https://github.com/mirage/mirage) on top of
[Solo5](https://github.com/Solo5/solo5) with 'urunc':

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/net-mirage-spt:latest
```

## Rumprun

[Rumprun](https://github.com/cloudkernels/rumprun) is a unikernel framework
based on NetBSD, providing support for a variety of POSIX-compliant
applications. [Rumprun](https://github.com/cloudkernels/rumprun) is
particularly useful for deploying existing POSIX applications with minimal
modifications. As a consequence of its design
[Rumprun](https://github.com/cloudkernels/rumprun) can be up-to-date with the
latest changes of NetBSD. However, the current repositories are not totally
up-to-date. The repository with the most recent NetBSD version is
[here](https://github.com/cloudkernels/rumprun).

In addition, [Rumprun](https://github.com/cloudkernels/rumprun) maintains a
[repository](https://github.com/cloudkernels/rumprun-packages) with all ported
applications that can be easily used on top of
[Rumprun](https://github.com/cloudkernels/rumprun).

### VMMs and other sandbox monitors

[Rumprun](https://github.com/cloudkernels/rumprun), as one of the oldest
unikernel frameworks, provides support for both Xen and KVM hypervisors.
Especially in the case of KVM,
[Rumprun](https://github.com/cloudkernels/rumprun) supports
[Qemu](https://www.qemu.org/) and [Solo5-hvt](https://github.com/Solo5/solo5).
It can access the network through virtio-net in the case of
[Qemu](https://qemu.org) and using [Solo5](https://github.com/Solo5/solo5)'s
I/O interface in the case of [Solo5](https://github.com/Solo5/solo5).  As far
as we concern, [Rumprun](https://github.com/cloudkernels/rumprun) only supports
block storage through virtio-block and
[Solo5](https://github.com/Solo5/solo5)'s I/O in [Qemu](https://qemu.org) and
[Solo5](https://github.com/Solo5/solo5) respectively.

Furthermore, [Rumprun](https://github.com/cloudkernels/rumprun) is also
possible to execute on top of [Solo5-spt](https://github.com/Solo5/solo5) a
sandbox monitor of [Solo5](https://github.com/Solo5/solo5) project that does
not use hardware-assisted virtualization. In that context,
[Rumprun](https://github.com/cloudkernels/rumprun) can access network and block
storage through [Solo5](https://github.com/Solo5/solo5)'s I/O interface.

### Rumprun and `urunc`

In the case of [Rumprun](https://github.com/cloudkernels/rumprun), `urunc`
provides support for [Solo5](https://github.com/Solo5/solo5) and
[Solo5](https://github.com/Solo5/solo5), but not yet for
[Qemu](https://qemu.org). For all monitors of
[Solo5](https://github.com/Solo5/solo5) `urunc` allows the access of both
network and block storage through [Solo5](https://github.com/Solo5/solo5)'s I/O
interface. In particular, `urunc` takes advantage of
[Rumprun](https://github.com/cloudkernels/rumprun) block storage and ext2
filesystem support and allows the mounting of the containerd's snapshot
directly in the unikernel. This is only possible using devmapper as a
snapshotter in containerd. For more information on setting up devmapper, please
take a look on our [installation
guide](../installation#setup-thinpool-devmapper).

Except for devmapper, `urunc` also supports the option of adding a block image
inside the container image and attaching it to
[Rumprun](https://github.com/cloudkernels/rumprun).

For more information on packaging
[Rumprun](https://github.com/cloudkernels/rumprun) unikernels for `urunc` take
a look at our [packaging](../package/) page.

An example of [Rumprun](https://github.com/cloudkernels/rumprun) on top of
[Solo5](https://github.com/Solo5/solo5) using a block image inside the
container's rootfs with 'urunc':

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/redis-hvt-rumprun-block:latest
```

An example of [Rumprun](https://github.com/cloudkernels/rumprun) on top of
[Solo5](https://github.com/Solo5/solo5) using devmapper with 'urunc':

```bash
sudo nerdctl run --rm -ti --snapshotter devmapper --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/redis-spt-rumprun-raw:latest
```

## Mewz

[Mewz](https://github.com/Mewz-project/Mewz) is a unikernel framework written
from scratch in Zig, targeting WASM workloads. In contrast to other WASM
runtimes that execute on top of general purpose operating systems,
[Mewz](https://github.com/Mewz-project/Mewz) is designed as a specialized
kernel where WASM applications can execute. In this way,
[Mewz](https://github.com/Mewz-project/Mewz) provides the minimal required
features and environment for executing WASM workloads. In addition, every WASM
application executes on a separate [Mewz](https://github.com/Mewz-project/Mewz)
instance, maintaining the single-purpose notion of unikernels.

According to the design of [Mewz](https://github.com/Mewz-project/Mewz), the
WASM application is transformed to an object file which is directly linked
against the [Mewz](https://github.com/Mewz-project/Mewz) kernel. Therefore,
when the [Mewz](https://github.com/Mewz-project/Mewz) kernel boots, it executes
the linked WASM application. [Mewz](https://github.com/Mewz-project/Mewz) has
partial support for [WASI](https://github.com/WebAssembly/WASI) and it provides
support for networking and an in-memory, read-only filesystem. In addition,
[Mewz](https://github.com/Mewz-project/Mewz) has socket compatibility with
[WasmEdge](https://github.com/WasmEdge/WasmEdge),

A few examples of [Mewz](https://github.com/Mewz-project/Mewz) unikernels can
be found in the [examples directory of Mewz's
repository](https://github.com/mewz-project/mewz/tree/main/examples).

### VMMs and other sandbox monitors

[Mewz](https://github.com/Mewz-project/Mewz) can execute only on top of
[Qemu](https://www.qemu.org/).  It can access the network through a virtio-net
PCI device. In the case of storage,
[Mewz](https://github.com/Mewz-project/Mewz) only supports an in-memory
read-only filesystem, which is directly linked along with the kernel.

### Mewz and `urunc`

In the case of [Mewz](https://github.com/Mewz-project/Mewz), `urunc` provides
support for [Qemu](https://www.qemu.org/). If the container is configured with
network access, then `urunc` will use a virtio-net PCI device to provide
network access to [Mewz](https://github.com/Mewz-project/Mewz) unikernels.

For more information on packaging
[Mewz](https://github.com/Mewz-project/Mewz) unikernels for `urunc` take
a look at our [packaging](../package/) page.

An example of [Mewz](https://github.com/Mewz-project/Mewz) on top of
[Qemu](https://qemu.org) with 'urunc':

```bash
sudo nerdctl run -m 512M --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/hello-server-qemu-mewz:latest
```

> Note: As far as we understand, Mewz requires at least 512M of memory to properly boot.

## Linux

[Linux](https://github.com/torvalds/linux) is maybe the most widely used kernel
and the vast majority of servers in the cloud use an OS based on
[Linux](https://github.com/torvalds/linux) kernel. As a result, most
applications and services we run on the cloud are built targeting
[Linux](https://github.com/torvalds/linux). Of course,
[Linux](https://github.com/torvalds/linux) is not a unikernel framework.
However, thanks to its highly configurable build-system we can create very
small, tailored [Linux](https://github.com/torvalds/linux) kernels for a single
application. The concept was introduced by the Lupine project, which examined
how we can turn the [Linux](https://github.com/torvalds/linux) kernel into a
unikernel.

Using [Linux](https://github.com/torvalds/linux), we can execute the vast
majority of the existing containers on top of `urunc`. However, the rational is
to target single application containers and not fully-blown distro containers.
Focusing on a single application, we can further minimize the
[Linux](https://github.com/torvalds/linux) kernel and keep only the necessary
components for a specific application. Such a design allows the creation of
minimal and fast single-application kernels that we can execute on top of
`urunc`.

### VMMs and other sandbox monitors

[Linux](https://github.com/torvalds/linux) has wide support for different
hardware and virtualization targets. It can execute on top of
[Qemu](https://qemu.org) and
[Firecracker](https://github.com/firecracker-microvm/firecracker). It can
access the network and storage through various ways (e.g. paravirtualization,
emulated devices etc.).

### Linux and `urunc`

Focusing on the single-application notion of using the
[Linux](https://github.com/torvalds/linux) kernel, `urunc` provides support for
both [Qemu](https://qemu.org) and
[Firecracker](https://github.com/firecracker-microvm/firecracker). For network,
`urunc` will make use of virtio-net either through PCI or MMIO, depending on
the monitor. In the case of storage, `urunc` can use initrd, virtio-block, 9pfs
or Virtiofs. In particular, `urunc` takes advantage of the extensive filesystem
support of [Linux](https://github.com/torvalds/linux) and can directly mount
containerd's snapshot directly to a [Linux](https://github.com/torvalds/linux)
VM. This is only possible using devmapper as a snapshotter in containerd. For
more information on setting up devmapper, please take a look on our
[installation guide](../installation#setup-thinpool-devmapper).

For more information on packaging applications and executing them on top of
[Linux](https://github.com/torvalds/linux) with `urunc` take a look at our
[running existing containers tutorial.](../tutorials/existing-container-linux)

An example of a Nginx alpine image on top of [Qemu](https://qemu.org) and
[Linux](https://github.com/torvalds/linux) with 'urunc' and devmapper as a
snapshotter:

```bash
sudo nerdctl run --rm -ti --snapshotter devmapper --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/nginx-qemu-linux-raw:latest
```

An example of a Redis alpine image transformed to a block file on top of
[Firecracker](https://github.com/firecracker-microvm/firecracker) and
[Linux](https://github.com/torvalds/linux) with 'urunc':

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/redis-firecracker-linux-block:latest
```

## Future unikernels and frameworks:

In the near future, we plan to add support for the following frameworks:

[OSv](https://github.com/cloudius-systems/osv): An OS designed specifically to
run as a single application on top of a hypervisor. OSv is known for its
performance optimization and supports a wide range of programming languages,
including Java, Node.js, and Python.
