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
  unikernel. Currently supported values: a) `qemu`, b) `firecracker`, c)
  `cloud-hypervisor`, d) `spt`,  e) `hvt`, f) `hyperlight-unikraft`.
- `com.urunc.unikernel.binary`: The path to the unikernel binary inside the
  container's rootfs. Optional for `hyperlight-unikraft`, whose `hluk` boots
  an embedded kernel when the image ships none.

Apart from the above, `urunc` accepts the following optional annotations:

- `com.urunc.unikernel.initrd`: The path to the initrd of the unikernel inside
  the container's rootfs.
- `com.urunc.unikernel.snapshot`: The path to a directory inside the
  container's rootfs holding a saved snapshot of the guest. Only supported by
  `hyperlight-unikraft`, in place of the `binary` and `initrd` annotations.
- `com.urunc.unikernel.unikernelVersion`: The version of the unikernel framework
  (e.g.  0.17.0).
- `com.urunc.unikernel.block`: The path to a block image inside container's
  rootfs, which will get attached to the unikernel.
- `com.urunc.unikernel.blkMntPoint`: The mount point of the block image to
  attach in the unikernel.
- `com.urunc.unikernel.mountRootfs`: A boolean value that if it is `true`,
  requests from `urunc` to mount the container's image rootfs in the unikernel
  (either as a block device or through shared-fs).
- `com.urunc.unikernel.hyperlightPorts`: The ports a `hyperlight-unikraft`
  guest may serve on, as a comma-separated list of ports and `LOW-HIGH` ranges
  (e.g. `8080,9000-9100`). Without it the guest serves on any port, as a
  container does. Only supported by `hyperlight-unikraft`.

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
[bunny](https://github.com/nubificus/bunny). Apart from building unikernels [bunny](https://github.com/nubificus/bunny)
can also pack existing unikernels (whether locally or from OCI images) as OCI images
for `urunc`. At its core [bunny](https://github.com/nubificus/bunny) leverages
[buildkit's LLB](https://github.com/moby/buildkit?tab=readme-ov-file#exploring-llb),
allowing us to create OCI images from any type of file. Currently [bunny](https://github.com/nubificus/bunny) can
process two formats of files: a) the typical Dockerfile-like syntax files and b)
`bunnyfile`, a specialized YAML-based file.

Bunny supports standard Containerfile/Dockerfile syntax, including `RUN`,
`ENV`, `ENTRYPOINT`, `CMD`, multi-stage builds and `COPY --from=`. It adds
`urunc` annotations and a `urunc.json` file to the resulting OCI image.

For Containerfile input, bunny also supplies the following defaults:

- The guest is Linux over QEMU unless the corresponding `com.urunc.unikernel.*`
  labels specify otherwise.
- If `com.urunc.unikernel.binary` is not set, bunny adds a kernel selected for
  the configured monitor (QEMU, Firecracker or Cloud Hypervisor).
- For Linux guests, bunny copies `/urunit` into the image and prepends it to
  the image's entrypoint. Set `LABEL bunny.urunit=false` to disable this when
  providing your own init process.

The YAML-based `bunnyfile` provides explicit fields for the platform, kernel,
rootfs and application configuration. The automatic kernel and urunit defaults
above apply to Containerfile input; a bunnyfile specifies those files explicitly.

The current syntax of `bunnyfile` is the following one:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest   # [1] Set bunnyfile syntax for automatic recognition from buildkit.
version: v0.1                                   # [2] Bunnyfile version.

platforms:                                      # [3] The target platform for building/packaging.
  framework: unikraft                           # [3a] The unikernel framework.
  version: v0.15.0                              # [3b] The version of the unikernel framework.
  monitor: qemu                                 # [3c] The hypervisor/VMM or any other kind of monitor.
  architecture: x86                             # [3d] The target architecture.

rootfs:                                         # [4] (Optional) Rootfs configuration.
  from: scratch                                 # [4a] scratch, local, or an OCI image reference.
  type: initrd                                  # [4b] initrd, block, or raw, subject to framework support.
  include:                                      # [4c] Files to copy into the rootfs.
    - config.txt:/etc/app.conf                  # Local source:destination shorthand.
    - from: nginx:alpine                        # Copy from another OCI image.
      source: /usr/share/nginx/html/index.html
      destination: /www/index.html

kernel:                                         # [5] Prebuilt kernel configuration.
  from: local                                   # [5a] local or an OCI image reference.
  path: kernel                                  # [5b] Path to the kernel in that source.

envs:                                           # [6] Environment variables as KEY=VALUE strings.
  - MESSAGE=hello
cmd: ["hello"]                                  # [7] Application command/arguments, like CMD.
entrypoint: ["/app"]                            # [8] Entrypoint, like ENTRYPOINT.

```

For an existing rootfs file, use `rootfs.from: local` (or an OCI image
reference) and `rootfs.path` to select the file. The `include` shorthand copies
local files; the mapping form selects a source with `from`, `source` and
`destination`. Use `from: local` in that mapping for the local build context.

`cmd` and `entrypoint` are lists, so each argument is a separate item. `envs`
is a list of `KEY=VALUE` strings. The old `cmdline` string is accepted for
backward compatibility when `cmd` is empty; use `cmd` in new files.

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
mode it outputs a LLB which can be passed to `buildctl`. Therefore,
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
can define a Dockerfile-like syntax file as:

```Dockerfile
#syntax=harbor.nbfc.io/nubificus/bunny:latest
FROM scratch

COPY nginx-qemu-x86_64-initrd_qemu-x86_64 /unikernel/kernel
COPY rootfs.cpio /unikernel/initrd

LABEL "com.urunc.unikernel.binary"=/unikernel/kernel
LABEL "com.urunc.unikernel.initrd"=/unikernel/initrd
LABEL "com.urunc.unikernel.unikernelType"="unikraft"
LABEL "com.urunc.unikernel.hypervisor"="qemu"
CMD ["nginx", "-c", "/nginx/conf/nginx.conf"]
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

cmd: ["nginx", "-c", "/nginx/conf/nginx.conf"]
```

and we can build it with a docker command:

```bash
docker build -f bunnyfile -t nubificus/urunc/nginx-unikraft-qemu:test .
```

> **NOTE**: We can use the above command and switch from bunnyfile to the
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
- annotations: a list with all the `urunc` annotations.

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
