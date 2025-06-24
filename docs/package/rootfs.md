---
layout: default
title: "Creating rootfs for unikernels"
description: "Packaging and creating unikernel's rootfs"
---

# Packaging and creating unikernel's rootfs for `urunc`

The unikernel and libOS ecosystem is highly diverse, with each framework
offering its own approach to storage. The users can easily get lost on the
various storage technologies that each framework supports.  This challenge was
one of the key reasons we created [bunny](https://github.com/nubificus/bunny) and [bunix](https://github.com/nubificus/bunix), in an effort to simplify the
process and provide a unified interface for managing storage across all
unikernel frameworks. On this page, we will explore the current state of our
tools and explain how to use them to create and package a root filesystem
(rootfs) for a unikernel.

For the time being, `urunc` supports three ways for passing the rootfs to the
unikernel: a) through initrd, b) as a virtio-block and c) through shared-fs.
In the virtio-block case,
`urunc` can either levarage the container's snapshot and pass the whole
container's rootfs as the rootfs, or `urunc` can make use of
a user-created file inside the OCI image to pass as a virtio-block device
to the unikernel.

Therefore, the users have the following options:

1. Manually create a rootfs (either initrd or block) and package it along with
   the unikernel.
2. Directly copy all the files to the container's rootfs and use devmapper
   snapshotter or shared-fs to pass the container's rootfs to the unikernel.
3. Let [bunny](https://github.com/nubificus/bunny) and [bunix](https://github.com/nubificus/bunix) create the rootfs file.

> **NOTE**: For the time being, [bunny](https://github.com/nubificus/bunny) supports the creation of initrd files and [bunix](https://github.com/nubificus/bunix) does not provide any support for creating the rootfs.

## Creating an initrd file

Some unikernel frameworks and guests support an in-memory ramfs as a rootfs. In
these cases, we can use [bunny](https://github.com/nubificus/bunny) and instruct it to create the rootfs for
us with all the specified files.
This feature is only supported using a `bunnyfile` and not a Dockerfile-like
syntax file.

Let's take a look at it using a Redis [Unikraft](https://unikraft.org) unikernel as an example, targeting
[Qemu](https://qemu.org). We will define the `bunnyfile` as:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest
version: v0.1

platforms:
  framework: unikraft
  monitor: qemu
  architecture: x86

rootfs:
  from: scratch
  type: initrd
  include:
  - redis.conf:/conf/redis.conf

kernel:
  from: local
  path: redis-qemu-x86_64-initrd_qemu-x86_64

cmdline: "redis-server /conf/redis.conf"
```

In the above file we specify the following:

- We want to package a [Unikraft](https://unikraft.org) unikernel that will execute on top of [Qemu](https://qemu.org) over
  x86 architecture.
- We want to create from `scratch` a rootfs with `initrd` as its type. In
  particular, we want a initrd file that contains the file `redis.conf` in
  `/data/conf/redis.conf`. In that way, [bunny](https://github.com/nubificus/bunny) creates the initrd file for us
  and sets up the respective `urunc` annotations to attach this initrd file
  when we boot the unikernel.
- We want to use the `redis-qemu-x86_64-initrd_qemu-x86_64` binary from the local build context as the unikernel to boot.
- We specify the cmdline for the unikernel as `redis-server /conf/redis.conf`

We can build the OCI image with the following command:

```bash
docker build -f bunnyfile -t urunc/prebuilt/redis-unikraft-qemu:test .
```

## Preparing an OCI image to be used as a rootfs for the unikernel

As previously mentioned, `urunc` is able to pass the whole container's rootfs
as the rootfs for the unikernels that support virtio-block or shared-fs. In
that scenario, we simply need to copy any local files into the OCI image's
rootfs. For this scenario we can use both
[bunny](https://github.com/nubificus/bunny) and
[bunix](https://github.com/nubificus/bunix).

> **NOTE**: In case the unikernel does not supports shared-fs (e.g. 9pfs), we
> can only use block devices and for that reason we need to create the unikernel
> container using devmapper as a snapshotter. In that way,`urunc` will use the
> snapshot of the container's image and directly attach it to the unikernel as  a
> block device.

As an example, we will use a Redis
[Rumprun](https://github.com/cloudkernels/rumprun) unikernel from
[Rumprun-packages](https://github.com/cloudkernels/rumprun-packages) targeting
[Solo5-hvt](https://github.com/Solo5/solo5).

> **NOTE**: [Rumprun](https://github.com/nubificus/rumprun) does not support
> attaching a block directly to `/`, hence `urunc` will instruct
> [Rumprun](https://github.com/nubificus/rumprun) to mount it at `/data`.

### Using `bunny`

In the case of [bunny](https://github.com/nubificus/bunny), we can use both supported
file syntaxes: a) `bunnyfile` and b) the Dockerfile-like syntax.

#### Using a `bunnyfile`

In order to package an existing pre-built unikernel image and any other files
with [bunny](https://github.com/nubificus/bunny) and a `bunnyfile` we can define the `bunnyfile` as:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest
version: v0.1

platforms:
  framework: rumprun
  monitor: hvt
  architecture: x86

rootfs:
  from: scratch
  type: raw
  include:
  - redis.conf:/conf/redis.conf

kernel:
  from: local
  path: redis.hvt

cmdline: "redis-server /data/conf/redis.conf"
```

In the above file we specify the following:

- We want to package a [Rumprun](https://github.com/nubificus/rumprun) unikernel
  that will execute on top of [Solo5-hvt](https://github.com/Solo5/solo5) over x86
  architecture.
- We want to create a rootfs from `scratch` with a `raw` type, meaning that
  we will just copy the
  specified files directly to the OCI image's rootfs. In particular, we copy the
  file `redis.conf` and place it at `/conf/redis.conf`.This is similar to
  `COPY` in Dockerfile.  Because of this type selection, [bunny](https://github.com/nubificus/bunny) will also set up
  the respective annotations to mount the OCI images rootfs directly to the
  unikernel.
- We want to use the `redis.hvt` binary as the unikernel to boot.
- We specify the cmdline for the unikernel as `redis-server  /data/conf/redis.conf`

We can build the OCI image with the following command:

```bash
docker build -f bunnyfile -t urunc/prebuilt/redis-rumprun-hvt:test .
```

#### Using a Dockerfile-like syntax

In order to package an existing pre-built unikernel image and any other files
with [bunny](https://github.com/nubificus/bunny) using a Dockerfile-like syntax file,
we can define the `Containerfile` as:

```
#syntax=harbor.nbfc.io/nubificus/bunny:0.2.0
FROM scratch

COPY redis.hvt /unikernel/redis.hvt
COPY redis.conf /conf/redis.conf

LABEL com.urunc.unikernel.binary=/unikernel/redis.hvt
LABEL "com.urunc.unikernel.cmdline"="redis-server /data/conf/redis.conf"
LABEL "com.urunc.unikernel.unikernelType"="rumprun"
LABEL "com.urunc.unikernel.hypervisor"="hvt"
LABEL "com.urunc.unikernel.mountRootfs"="true"

```

In the above file:

- We directly copy the unikernel binary and any files that we want to have in
  the OCI's image rootfs.
- We manually specify through labels the necessary `urunc` annotations,
  including the `com.urunc.unikernel.mountRootfs` which instructs `urunc` to
  attach the container snapshot as a block device to the unikernel.

We can build the OCI image with the following command:

```bash
docker build -f Containerfile -t urunc/prebuilt/redis-rumprun-hvt:test .
```

### Using `bunix`

In the case of [bunix](https://github.com/nubificus/bunix) we need the whole repository in the same directly as
the unikernel. Then, we simply need to edit the `args.nix` file. For our
pre-built Redis [Rumprun](https://github.com/nubificus/rumprun) unikernel we can define the files as:

```Nix
{
  name = "urunc/prebuilt/redis-rumprun-hvt";
  tag = "test";
  files = {
    "./redis.hvt" = "/unikernel/redis.hvt";
    "./redis.conf" = "/conf/redis.conf";
  };
  annotations = {
    unikernelType = "rumprun";
    hypervisor = "hvt";
    binary = "/unikernel/redis.hvt";
    cmdline = "hello";
    unikernelVersion = "";
    initrd = "";
    block = "";
    blkMntPoint = "";
    mountRootfs = "true";
  };
}
```

In the above file:

- We directly specify the files to copy inside the OCI's image rootfs.
- We manually specify through labels the necessary `urunc` annotations,
  including the `com.urunc.unikernel.mountRootfs` which instructs `urunc` to
  attach the container snapshot as a block device to the unikernel.

We can build the OCI image by simply running the following command:

```bash
nix-build default.nix
```

The above command will create a container image in a tar inside Nix's store. For
easier access of the tar, Nix creates a symlink of the tar file in the CWD. The
symlink will be named as `result`. Therefore, we can load the container image with:

```bash
docker load < result
```

## Packaging a pre-built rootfs along with the unikernel

At last, there is always the option to manually create the rootfs file for the
unikernel and then package the unikernel binary and the rootfs file setting up
the respective annotations.

As an example, we will use a simple  [C HTTP Web
Server](https://github.com/unikraft/catalog/tree/main/examples/http-c) from
[Unikraft's catalog](https://github.com/unikraft/catalog).

### Using `bunny`

In the case of [bunny](https://github.com/nubificus/bunny), we can use both supported
file syntaxes: a) `bunnyfile` and b) the Dockerfile-like syntax.

#### Using a `bunnyfile`

In order to package an existing pre-built unikernel and its rootfs
with [bunny](https://github.com/nubificus/bunny) and a `bunnyfile` we can define the `bunnyfile` as:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest
version: v0.1

platforms:
  framework: unikraft
  monitor: qemu
  architecture: x86

rootfs:
  from: local
  path: rootfs.cpio

kernel:
  from: local
  path: app-elfloader-qemu-x86_64-initrd_qemu-x86_64

cmdline: "/chttp"
```

In the above file we specify the following:

- We want to package a [Unikraft](https://unikraft.org) unikernel that will execute
  on top of [Qemu](https://qemu.org) over x86 architecture.
- We want to use a local file as a rootfs, specifically the `rootfs.cpio` file
  in the local build context.
- We want to use the `app-elfloader-qemu-x86_64-initrd_qemu-x86_64` binary as
  the unikernel to boot.
- We specify the cmdline for the unikernel as `/chttp`

We can build the OCI image with the following command:

```bash
docker build -f bunnyfile -t urunc/prebuilt/chttp-unikraft-qemu:test .
```

#### Using a Dockerfile-like syntax

We can do all the above using a Dockerfile-like syntax file as:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest
FROM scratch

COPY app-elfloader-qemu-x86_64-initrd_qemu-x86_64 /unikernel/kernel
COPY rootfs.cpio /unikernel/rootfs.cpio

LABEL "com.urunc.unikernel.binary"=/unikernel/kernel
LABEL "com.urunc.unikernel.initrd"="/unikernel/rootfs.cpio"
LABEL "com.urunc.unikernel.cmdline"="/chttp"
LABEL "com.urunc.unikernel.unikernelType"="unikraft"
LABEL "com.urunc.unikernel.hypervisor"="qemu"
```

In the above file:

- We directly copy the unikernel binary and the cpio file in
  the OCI's image rootfs.
- We manually specify all `urunc` annotations, including the initrd one to
  specify the file to use as initrd for the unikernel.

We can build the OCI image with the following command:

```bash
docker build -f Containerfile -t urunc/prebuilt/chttp-unikraft-qemu:test .
```

### Using `bunix`

In the case of [bunix](https://github.com/nubificus/bunix) we need the whole repository in the same directory as
the unikernel and the cpio file. Then, we simply need to edit the `args.nix` file as:

```Nix
{
  name = "urunc/prebuilt/chttp-unikraft-qemu";
  tag = "test";
  files = {
    "./app-elfloader-qemu-x86_64-initrd_qemu-x86_64" = "/unikernel/kernel";
    "./rootfs.cpio" = "/unikernel/rootfs.cpio";
  };
  annotations = {
    unikernelType = "unikraft";
    hypervisor = "qemu";
    binary = "/unikernel/kernel";
    cmdline = "/chttp";
    unikernelVersion = "";
    initrd = "/unikernel/rootfs.cpio";
    block = "";
    blkMntPoint = "";
    mountRootfs = "";
  };
}
```

In the above file:

- We directly specify the files to copy inside the OCI's image rootfs.
- We manually specify all `urunc` annotations, including the initrd one to
  specify the file to use as initrd for the unikernel.

We can build the OCI image by simply running the following command:

```bash
nix-build default.nix
```

The above command will create a container image in a tar inside Nix's store. For
easier access of the tar, Nix creates a symlink of the tar file in the CWD. The
symlink will be named as `result`. Therefore, we can load the container image with:

```bash
docker load < result
```
