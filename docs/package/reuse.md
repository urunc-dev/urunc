---
layout: default
title: "Reusing unikernels from OCI images"
description: "Reusing OCI images that contain unikernels"
---

# Reusing OCI images that contain unikernels

In this page we will explain how we can reuse existing OCI images that contain
unikernels to either update or append `urunc` annotations. As an
example, we will use an existing [Unikraft](https://unikraft.org) Unikernel
image from [Unikraft's catalog](https://github.com/unikraft/catalog), The goal
will be to transform this image to an OCI image that `urunc` can handle, by
simply appending the necessary annotations.

Currently only `bunny` supports reusing an existing OCI image. However, both
file formats, `bunnyfile` and Dockerfile-like syntax files, can be used.

> **NOTE**: The below steps can be easily adjusted to any existing OCI image.

## Using a `bunnyfile`

In order to append `urunc` annotations in an existing [Unikraft](https://unikraft.org) OCI image,
we can define the `bunnyfile` as:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest
version: v0.1

platforms:
  framework: unikraft
  monitor: qemu
  architecture: x86

kernel:
  from: unikraft.org/nginx:1.15
  path: /unikraft/bin/kernel

cmdline: "nginx -c /nginx/conf/nginx.conf"
```

In the above file we specify the followings:

- We want to use a [Unikraft](https://unikraft.org) unikernel that will execute on top of Qemu over x86
  architecture.
- We want to use the unikernel binary `/unikraft/bin/kernel` from the
  `unikraft.org/nginx:1.15` OCI image.
- We specify the cmdline for the unikernel as `nginx -c /nginx/conf/nginx.conf"`

With the above file, `bunny` will fetch the OCI image and append the `urunc`
annotations. We can build the OCI image with the following command:

```bash
docker build -f bunnyfile -t urunc/reuse/nginx-unikraft-qemu:test .
```

## Using a Dockerfile-like syntax

In the case of the Dockerfile-like syntax file, we need to manually specify the
`urunc` annotations, using the respective labels. Therefore, to transform the
above `bunnyfile` to the equivalent `Containerfile`:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest
FROM unikraft.org/nginx:1.15

LABEL com.urunc.unikernel.binary="/unikraft/bin/kernel"
LABEL "com.urunc.unikernel.unikernelType"="unikraft"
LABEL "com.urunc.unikernel.hypervisor"="qemu"
CMD ["nginx", "-c", "/nginx/conf/nginx.conf"]
```

In the above file:

- We set the `unikraft.org/nginx:1.15` as the base for our OCI image.
- We manually specify through labels the `urunc` annotations.

We can build the OCI image with the following command:

```bash
docker build -f Containerfile -t urunc/prebuilt/nginx-unikraft-qemu:test .
```
