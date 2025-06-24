The [OCI (Open Container Initiative) image
format](https://github.com/opencontainers/image-spec) is a standardized
specification for packaging and distributing containerized applications across
different platforms and container runtimes. It defines a common structure for
container images, including their metadata, layers, and filesystem content.

Since `urunc` is an OCI-compatible container runtime, it expects the unikernel
to be placed inside an OCI container image. Nevertheless, in order to
differentiate between traditional container images and unikernel OCI images,
`urunc` makes use of annotations or a metadata file (`urunc.json`) inside the
container's rootfs.

To facilitate the process, we provide various tools that build and package a unikernel
binary, along with the application's necessary files in a container image and
set the respective annotations. In particular, we can produce an OCI image with
all `urunc`'s annotations using:

1. [bunny](https://github.com/nubificus/bunny) a tool that builds and packages unikernels
    using buildkit's LLB and can also act as a frontend for
   [buildkit](https://github.com/moby/buildkit?tab=readme-ov-file#output).
2. [bunix](https://github.com/nubificus/bunix) which uses [Nix
   packages](https://github.com/NixOS/nix) to package a unikernel as an OCI image.

In this section, we will first explain all the annotations that `urunc`
expects, in order to handle unikernels and describe how to build and package
unikernels as OCI images using the aforementioned tools.

**Quick links:**

- [Packaging pre-built unikernels](../package/pre-built)
- [Using unikernels from existing OCI images](../package/reuse)
- [Packaging and creating unikernel's rootfs](../package/rootfs)

## Annotations

[OCI
annotations](https://github.com/opencontainers/image-spec/blob/main/annotations.md)
are key-value metadata used to describe and provide additional
context for container images and runtime configurations within the OCI
specification. Using these annotations developers can
embed non-essential information about containers, such as version details,
licensing, build information, or custom runtime parameters, without affecting
the core functionality of the container itself.
The annotations can be placed in several components of the specification.
However, in the case of `urunc` we are interested about annotations which can
reach the container runtime.

Using these annotations `urunc` receives information regarding the type of the
unikernel, the VMM or sandbox mechanism to use and more. For the time being, the
required annotations are the following:

- `com.urunc.unikernel.unikernelType`: The type of the unikernel. Currently
  supported values: a) unikraft, b) rumprun, c) mirage.
- `com.urunc.unikernel.hypervisor`: The VMM or sandbox monitor to run the
  unikernel Currently supported values: a) `qemu`, b) `firecracker`, c) `spt`,
  d) `hvt`.
- `com.urunc.unikernel.binary`: The path to the unikernel binary inside the
  container's rootfs
- `com.urunc.unikernel.cmdline`: The application's cmdline to pass to the
  unikernel.

Except of the above, `urunc` accepts the following optional annotations:

- `com.urunc.unikernel.initrd`: The path to the initrd of the unikernel inside
  the container's rootfs.
- `com.urunc.unikernel.unikernelVersion`: The version of the unikernel framework
  (e.g.  0.17.0).
- `com.urunc.unikernel.block`: The path to a block image inside container's
  rootfs, which will get attached to the unikernel.
- `com.urunc.unikernel.blkMntPoint`: The mount point of the block image to
  attach in the unikernel.
- `com.urunc.unikernel.mountRootfs`: A boolean value that if it is `true`,
  requests from `urunc` to mount the container's image rootfs in the unikernel
  (either as a block device or through shared-fs).

Due to the fact that [Docker](https://www.docker.com/) and some high-level
container runtimes do not pass the image annotations to the underlying container
runtime, `urunc` can also read the above information from a file inside the
container's rootfs. The file should be named `urunc.json`, it should be
placed in the root directory of the container's rootfs and it should have a JSON
format with the above information, where the values are base64 encoded.

## Tools to construct OCI images with `urunc`'s annotations

As previously mentioned we currently provide 2 different tools to build and
package unikernels in OCI images with `urunc`'s annotations.

### bunny

In an effort to simplify the process of building various unikernels, we built
[bunny](https://github.com/nubificus/bunny). Except of building unikernels [bunny](https://github.com/nubificus/bunny)
can also pack existing unikernels (whether locally or from OCI images) as OCI images
for `urunc`. At its core [bunny](https://github.com/nubificus/bunny) leverages
[buildkit's LLB](https://github.com/moby/buildkit?tab=readme-ov-file#exploring-llb),
allowing us to create OCI images from any type of file. Currently [bunny](https://github.com/nubificus/bunny) can
process two formats of files: a) the typical Dockerfile-like syntax files and b)
`bunnyfile`, a specialized YAML-based file.

When using Dockerfile-like files, [bunny](https://github.com/nubificus/bunny)
can only package pre-built unikernel images; it cannot build them. This format
is primarily retained for compatibility with pun and bima, which are no longer
maintained. Currently, [bunny](https://github.com/nubificus/bunny)
can handle the following *instructions*:

- `FROM`: Specify an existing OCI image to use as a base.
- `COPY`: this works as in Dockerfiles. At this moment, only a single copy
  operation per *instruction* (think one copy per line). These files are copied
  inside the container's image rootfs.
- `LABEL`: all LABEL *instructions* are added as annotations to the container's
  image. They are also added to a special `urunc.json` inside the container's image
  rootfs.

To further extend the functionality and provide a common interface to facilitate
unikernel building, we defined `bunnyfile`. It is a YAML-based special file that
[bunny](https://github.com/nubificus/bunny) transforms to LLB with all the
necessary steps to build the respective unikernel. Except of building
unikernels, [bunny](https://github.com/nubificus/bunny) can also be used to build or append files in the unikernel's
rootfs.

The current syntax of `bunnyfile` is the following one:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest   # [1] Set bunnyfile syntax for automatic recognition from buildkit.
version: v0.1                                   # [2] Bunnyfile version.

platforms:                                      # [3] The target platform for building/packaging.
  framework: unikraft                           # [3a] The unikernel framework.
  version: v0.15.0                              # [3b] The version of the unikernel framework.
  monitor: qemu                                 # [3c] The hypervisor/VMM or any other kind of monitor.
  architecture: x86                             # [3d] The target architecture.

rootfs:                                         # [4] (Optional) Specifies the rootfs of the unikernel.
  from: local                                   # [4a] (Optional) The source of the rootfs.
  path: initrd                                  # [4b] (Required if from is not scratch) The path in the source, where the prebuilt rootfs file resides.
  type: initrd                                  # [4c] (optional) The type of rootfs (e.g. initrd, raw, block)
  include:                                      # [4d] (Optional) A list of local files to include in the rootfs
    - src:dst

kernel:                                         # [5] Specify a prebuilt kernel to use
  from: local                                   # [5a] Specify the source of a prebuilt kernel.
  path: kernel                                  # [5b] The path where the kernel image resides.

cmdline: hello                                  # [6] The cmdline of the app.

```

For more information regarding the `bunnyfile` please take a look at the
respective section of [bunny's
README](https://github.com/nubificus/bunny?tab=readme-ov-file#the-bunnyfile).
Furthermore, you can find various different examples and use cases for
[bunny](https://github.com/nubificus/bunny) in the [examples directory of
bunny's repository](https://github.com/nubificus/bunny/tree/main/examples).

#### Packaging a unikernel with bunny

Since [bunny](https://github.com/nubificus/bunny) uses
[buildkit](https://github.com/moby/buildkit?tab=readme-ov-file#output) it
supports two modes of execution. In the first mode it acts as a [buildkit
frontend](https://docs.docker.com/build/buildkit/frontend/) and in the second
mode it outputs a LLB which can be passed to `buildctl`.Therefore,
[bunny](https://github.com/nubificus/bunny) depends on
[buildkit](https://github.com/moby/buildkit?tab=readme-ov-file#output) which
should be installed. However, if [docker](https://www.docker.com/) is already
installed, the frontend execution mode of [bunny](https://github.com/nubificus/bunny)
can be used directly without building or installing anything.

It is important to note that if we
want to use [bunny](https://github.com/nubificus/bunny) as a frontend for buildkit
we need to start the Containerfile with the following line:

```Dockerfile
#syntax=harbor.nbfc.io/nubificus/bunny:<version>
```

***Using a Dockerfile-like syntax file***

If we want to package a locally built Nginx Unikraft unikernel, we
can define the a Dockerfile-like syntax file as:

```Dockerfile
#syntax=harbor.nbfc.io/nubificus/bunny:latest
FROM scratch

COPY nginx-qemu-x86_64-initrd_qemu-x86_64 /unikernel/kernel
COPY rootfs.cpio /unikernel/initrd

LABEL "com.urunc.unikernel.binary"=/unikernel/kernel
LABEL "com.urunc.unikernel.initrd"=/unikernel/initrd
LABEL "com.urunc.unikernel.cmdline"="nginx -c /nginx/conf/nginx.conf"
LABEL "com.urunc.unikernel.unikernelType"="unikraft"
LABEL "com.urunc.unikernel.hypervisor"="qemu"
```

***Using bunnyfile***

If we want to package the same unikernel, using `bunnyfile`, we have to
define the file as:

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
  path: nginx-qemu-x86_64-initrd_qemu-x86_64

cmdline: nginx -c /nginx/conf/nginx.conf
```

and we can build it with a docker command:

```bash
docker build -f bunnyfile -t nubificus/urunc/nginx-unikraft-qemu:test .
```

> **NOTE**: We can use the above command and switch form bunnyfile to the
> Dockerfile-like file and build the same unikernel OCI image.

For more information check [bunny's README](https://github.com/nubificus/bunny?tab=readme-ov-file#bunny-build-and-package-unikernels-like-containers).

### bunix

For Nix users, we have created a set of Nix scripts that we maintain in the
[bunix](https://github.com/nubificus/bunix) repository to build container
images for `urunc`. In contrast to the previous tools,
[bunix](https://github.com/nubificus/bunix) uses a nix file to define the
files to package as a container image, along with the `urunc` annotations. In
particular, this file is the `args.nix` file, which expects the same fields:

- name: the name of the container image that Nix will build
- tag: the tag of the container image that Nix will build
- files: a list of key-value pairs with all the files to copy inside the
  container image. The key-value pairs have the following format:
  `"<path-based-on-cwd>" = "<path-inside-container>"`.
- annotations: a list will all the `urunc` annotations.

#### Packaging a unikernel with bunix

A necessary requirement to use [bunix](https://github.com/nubificus/bunix)
is the presence of [Nix package manager](https://github.com/NixOS/nix). Then
using [bunix](https://github.com/nubificus/bunix) is as simple as completing
the `args.nix` file.

For example to package a locally built Rumprun Hello world unikernel running on
top of Solo5-hvt, we should set the `args.nix` file as:

```Nix
{
  name = "nginx-unikraft-qemu";
  tag = "test";
  files = {
    "./nginx-qemu-x86_64-initrd_qemu-x86_64" = "/unikernel/kernel";
    "./rootfs.cpio" = "/unikernel/initrd";
  };
  annotations = {
    unikernelType = "unikraft";
    hypervisor = "qemu";
    binary = "/unikernel/kernel";
    cmdline = "nginx -c /nginx/conf/nginx.conf";
    unikernelVersion = "";
    initrd = "/unikernel/initrd";
    block = "";
    blkMntPoint = "";
  };
}
```

Then we can build the image by simply running the following command
inside the repository:

```bash
nix-build default.nix
```

The above command will create a container image in a tar inside Nix's store. For
easier access of the tar, Nix creates a symlink of the tar file in the CWD. The
symlink will be named as `result`. Therefore, we can load the container image with:

```bash
docker load < result
```

Please check [bunix's README](https://github.com/nubificus/bunix) for more information.
