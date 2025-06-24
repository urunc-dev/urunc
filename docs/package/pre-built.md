---
layout: default
title: "Pre-built unikernels"
description: "Packaging pre-built unikernels"
---

# Packaging pre-built unikernels for `urunc`

In this page we will explain the process of packaging an existing / pre-built
unikernel as an OCI image with the necessary annotations for `urunc`. As an
example, we will use a network example over
[MirageOS](https://github.com/mirage) from
[mirage-skeleton](https://github.com/mirage/mirage-skeleton/tree/main/device-usage/network)
targeting [Solo5-hvt](https://github.com/Solo5/solo5).

For simply packaging pre-built unikernel images, we can use both
[bunny](https://github.com/nubificus/bunny) and
[bunix](https://github.com/nubificus/bunix).

> **NOTE**: The below steps can be easily adjusted to any pre-built unikernel image.

## Using `bunny`

In the case of [bunny](https://github.com/nubificus/bunny) and pre-built
unikernel images, we can use both supported file syntaxes: a) `bunnyfile` and
b) the Dockerfile-like syntax.

### Using a `bunnyfile`

In order to package an existing pre-built unikernel image with [bunny](https://github.com/nubificus/bunny) and a
`bunnyfile` we can define the `bunnyfile` as:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest
version: v0.1

platforms:
  framework: mirage
  monitor: hvt
  architecture: x86

kernel:
  from: local
  path: network.hvt

cmdline: ""
```

In the above file we specify the following:

- We want to package a [MirageOS](https://github.com/mirage) unikernel that
  will execute on top of [Solo5-hvt](https://github.com/Solo5/solo5) over x86
  architecture.
- We want to use the `network.hvt` binary as the unikernel to boot.
- We do not specify any command line, since the unikernel does not necessarily require one.

We can build the OCI image with the following command:

```bash
docker build -f bunnyfile -t urunc/prebuilt/network-mirage-hvt:test .
```

### Using a Dockerfile-like syntax

In order to package an existing pre-built unikernel image with
[bunny](https://github.com/nubificus/bunny) and a Dockerfile-like syntax file,
we can define the `Containerfile` as:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest
FROM scratch

COPY network.hvt /unikernel/network.hvt

LABEL com.urunc.unikernel.binary=/unikernel/network.hvt
LABEL "com.urunc.unikernel.cmdline"=""
LABEL "com.urunc.unikernel.unikernelType"="mirage"
LABEL "com.urunc.unikernel.hypervisor"="hvt"
LABEL "com.urunc.unikernel.mountRootfs"="false"
```

In the above file:

- We directly copy the unikernel binary in the OCI's image rootfs.
- We manually specify through labels the `urunc` annotations.

We can build the OCI image with the following command:

```bash
docker build -f Containerfile -t urunc/prebuilt/network-mirage-hvt:test .
```

## Using `bunix`

In the case of [bunix](https://github.com/nubificus/bunix) we need to clone the whole
repository in the same directly as the
unikernel. Then, we simply need to edit the `args.nix` file as:

```Nix
{
  name = "urunc/prebuilt/network-mirage-hvt";
  tag = "test";
  files = {
    "./network.hvt" = "/unikernel/network.hvt";
  };
  annotations = {
    unikernelType = "mirage";
    hypervisor = "hvt";
    binary = "/unikernel/network.hvt";
    cmdline = "";
    unikernelVersion = "";
    initrd = "";
    block = "";
    blkMntPoint = "";
    mountRootfs = "false";
  };
}
```

In the above file:

- We directly specify the files to copy inside the OCI's image rootfs.
- We manually specify all `urunc` annotations.

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
