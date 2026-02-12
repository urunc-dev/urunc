# Supported VMMs and software-based monitors

One of the main goals of `urunc` is to be a generic OCI unikernel runtime for
various unikernel frameworks and similar technologies. In order to achieve
that, we want to support as many Virtual Machine Monitors (VMMs) and other
types of sandboxing mechanisms such as user-space monitors based on
[seccomp](https://en.wikipedia.org/wiki/Seccomp).

In this document, we will go through the current state of `urunc`'s support for
VMMs and monitors that utilize software-based isolation technologies. We will
provide a brief description about them, along with installation instructions
and a few comments regarding their integration with `urunc`.

> Note: In general, `urunc` expects all supported VM/Sandbox monitors to be available
somewhere in the `$PATH`.

## Virtual Machine Monitors (VMMs)

VMMs use hardware-assisted virtualization technologies in order to create a
Virtual Machine (VM) where a guest OS will execute. It is one of the most
widely used technology for providing strong isolation in multi-tenant
environments. For the time being `urunc` supports 3 types of such VMMs: 1)
[Qemu](https://www.qemu.org/), 2)
[Firecracker](https://firecracker-microvm.github.io/) and 3) [Solo5-hvt](https://github.com/Solo5/solo5).

### Qemu

[Qemu](https://www.qemu.org/) (Quick Emulator) is an open-source virtualization
platform that enables the emulation of various hardware architectures. By
leveraging Linux's KVM, [Qemu](https://www.qemu.org/) is able to create VMs and
manage their execution.  Some of the biggest advantages of
[Qemu](https://www.qemu.org/) are the mature and stable interface and codebase.
In addition, [Qemu](https://www.qemu.org/) supports various paravirtual
devices, mostly based on
[VirtIO](https://docs.oasis-open.org/virtio/virtio/v1.2/virtio-v1.2.html) and
allows the direct use of the host's devices with passthrough.

#### Installing Qemu

We can easily install [Qemu](https://www.qemu.org/) through almost all package
managers. For more details check [Qemu's download
page](https://www.qemu.org/download#linux). For instance, in the case of
Ubuntu, we can simply run the following command:
```bash
sudo apt-get install qemu-system
```

#### Qemu and `urunc`

In the case of [Qemu](https://www.qemu.org/), `urunc` makes use of its
`virtio-net` device to provide network support for the unikernel through a tap
device. In addition, `urunc` can leverage [Qemu](https://www.qemu.org/)'s
initrd option in order to provide the Unikernel with an initial RamFS
(initramfs). However, [Qemu](https://www.qemu.org/) supports various ways to
provide storage in VMs such as block devices through virtio-blk,
shared-fs through 9pfs, virtiofs and initramfs.

We plan to add support for all the above options, but as previously mentioned
only Initramfs is supported for the time being.

Supported unikernel frameworks with `urunc`:

- [Unikraft](../unikernel-support#unikraft)
- [MirageOS](../unikernel-support#mirage)
- [Mewz](../unikernel-support#mewz)
- [Linux](../unikernel-support#linux)

An example unikernel:

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/nginx-qemu-unikraft-initrd:latest
```

### AWS Firecracker

AWS [Firecracker](https://firecracker-microvm.github.io/) is an open-source
virtualization technology developed by Amazon Web Services (AWS) that is
designed to run serverless workloads efficiently.
[Firecracker](https://firecracker-microvm.github.io/) provides a minimalist
VMM, allowing the creation of lightweight virtual machines, called microVMs,
that are faster and more resource-efficient than traditional VMs. In contrast
with [Qemu](https://www.qemu.org/),
[Firecracker](https://firecracker-microvm.github.io/) aims to provide a smaller
set of devices for the VMs. The main benefit of Firecracker comes from its fast
VM instantiation and guest OS boot.

#### Installing Firecracker

[Firecracker](https://firecracker-microvm.github.io/) is not available through
a package manager, but it can easily be installed. The [Getting
Started](https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md) guide
of [Firecracker](https://firecracker-microvm.github.io/) describes how users
can set up [Firecracker](https://firecracker-microvm.github.io/). Long story short,
we can fetch a
[Firecracker](https://firecracker-microvm.github.io/) binary with the following
commands:

```bash
ARCH="$(uname -m)" 
VERSION="v[[ versions.firecracker ]]"
release_url="https://github.com/firecracker-microvm/firecracker/releases"
curl -L ${release_url}/download/${VERSION}/firecracker-${VERSION}-${ARCH}.tgz | tar -xz
# Rename the binary to "firecracker"
sudo mv release-${VERSION}-$(uname -m)/firecracker-${VERSION}-${ARCH} /usr/local/bin/firecracker
rm -fr release-${VERSION}-$(uname -m)
```

It is important to note that `urunc` expects to find the `firecracker` binary
located in the `$PATH` and named `firecracker`.

> Note: Since only Unikraft can boot on top of Firecracker (from the supported
unikernels in `urunc`) we use the v[[ versions.firecracker ]] version of
[Firecracker](https://firecracker-microvm.github.io/), due to some [booting
issues](https://github.com/unikraft/unikraft/issues/1410) of Unikraft in newer
versions.

#### Firecracker and `urunc`

In the case of [Firecracker](https://firecracker-microvm.github.io/), `urunc`
makes use of its `virtio-net` device to provide network support for the
unikernel though a tap device. In addition, `urunc` can leverage
[Firecracker](https://firecracker-microvm.github.io/)'s initrd option in order
to provide the Unikernel with an initial RamFS (initramfs).
[Firecracker](https://firecracker-microvm.github.io/) does not support
shared-fs between the host and the guest. However, it does provide support for
virtio-block.

We plan to add support for virtio-block, but as previously mentioned only
Initramfs is supported for the time being.

Supported unikernel frameworks with `urunc`:

- [Unikraft](../unikernel-support#unikraft)
- [Linux](../unikernel-support#linux)

An example unikernel:

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/nginx-firecracker-unikraft-initrd:latest
```

### Solo5-hvt

[Solo5-hvt](https://github.com/Solo5/solo5) is a lightweight, high-performance
VMM designed to run unikernels in a virtualized environment. As a part of the
broader Solo5 project, [Solo5-hvt](https://github.com/Solo5/solo5) provides a
minimal, efficient abstraction layer for running unikernels on modern hardware,
leveraging hardware virtualization technologies Some of the key benefits of
[Solo5-hvt](https://github.com/Solo5/solo5) is its simplicity and extremely
fast boot times of unikernels. In contrast to the other VMMs,
[Solo5-hvt](https://github.com/Solo5/solo5) does not provide support for virtIO
devices. Instead, it defines its own interface, which can be used for network
and block I/O.

#### Installing Solo5-hvt

Solo5 can be installed by building from source. However, in order to do that,
we will need a few packages.

```bash
sudo apt install libseccomp-dev pkg-config build-essential
```

Next, we can clone and build `solo5-hvt`.

```bash
git clone -b v[[ versions.solo5 ]] https://github.com/Solo5/solo5.git
cd solo5
./configure.sh && make -j$(nproc)
```

It is important to note that `urunc` expects to find the `solo5-hvt` binary
located in the `$PATH` and named as `solo5-hvt`. Therefore, to install it:

```bash
sudo cp tenders/hvt/solo5-hvt /usr/local/bin
```

#### Solo5-hvt and `urunc`

In the case of [Solo5-hvt](https://github.com/Solo5/solo5), `urunc` supports
all the devices and utilizes a tap device to provide network in the unikernel.
For the storage part, `urunc` supports the block storage interface of
[Solo5-hvt](https://github.com/Solo5/solo5), which can be used in two ways,
either with a block image inside the container image, or using the devmapper as
a snapshotter.

In the first case, we copy inside the container image a block image that
contains all the data we want to pass in the unikernel.

In the second case, we copy directly all the files we want the unikernel to
access inside the container's image. Using devmapper `urunc` will use the
container's image snapshot as a block image for the unikernel. It is important
to note that the unikernel framework must support the respective filesystem
type (e.g. ext2/3/4). This is the case for Rumprun unikernel.

Supported unikernel frameworks with `urunc`:

- [Rumprun](../unikernel-support#rumprun)
- [MirageOS](../unikernel-support#mirage)

An example unikernel with a block image inside the container's rootfs:

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/redis-hvt-rumprun-block:latest
```

## Software-based isolation monitors

Except for the traditional VM-based isolation solutions, there are other
solutions which provide isolation using software-based technologies too. In
that case the monitor interacts with a user-space kernel on top of which the
application is running. The user-space kernel intercepts or defines a set of
system calls and then forwards them to the monitor. To further strengthen
security, it is common to use seccomp filters to limit the exposure of the host
OS to the monitor.

A well-known example of such a technology is [gVisor](https://gvisor.dev/).
Unfortunately, gVisor does not support the execution of any unikernel
framework.

### Solo5-spt

In a similar way,
[Solo5-spt](https://github.com/Solo5/solo5) is a specialized backend for the
Solo5 project, designed to run unikernels in systems that do not have access to
hardware-assisted virtualization technologies.
[Solo5-spt](https://github.com/Solo5/solo5) executes a unikernel monitor with a
seccomp filter allowing only seven system calls. The unikernel running on top of Solo5-spt
interacts with this monitor through a similar interface with
[Solo5-hvt](https://github.com/Solo5/solo5), facilitating network and block
storage I/O. [Solo5-spt](https://github.com/Solo5/solo5) can provide extremely
fast instantiation times, very small overhead, along with performant execution.

#### Installing Solo5-spt

The installation process of [Solo5-spt](https://github.com/Solo5/solo5) is
similar with the [Solo5-hvt](https://github.com/Solo5/solo5) one. In fact, both
projects share the same repository. Hence we can follow the same steps as in
Solo5-hvt. At first, make sure to install the necessary packages.

```bash
sudo apt install libseccomp-dev pkg-config build-essential
```

Next, we can clone and build `solo5-spt`.

```bash
git clone -b v[[ versions.solo5 ]] https://github.com/Solo5/solo5.git
cd solo5
./configure.sh && make -j$(nproc)
```

It is important to note that `urunc` expects to find the `solo5-spt` binary
located in the `$PATH` and named `solo5-spt`. Therefore, to install it:

```bash
sudo cp tenders/spt/solo5-spt /usr/local/bin
```

#### Solo5-spt and `urunc`

Similarly with [Solo5-hvt](https://github.com/Solo5/solo5), `urunc` supports
all the devices of [Solo5-spt](https://github.com/Solo5/solo5). For more
information take a look at the respective [Solo5-hvt
section](#solo5-hvt-and-urunc).

Supported unikernel frameworks with `urunc`:

- [Rumprun](../unikernel-support#rumprun)
- [MirageOS](../unikernel-support#mirage)

An example unikernel which utilizes devmapper for block storage:

```bash
sudo nerdctl run --rm -ti --snapshotter devmapper --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/redis-spt-rumprun-raw:latest
```
