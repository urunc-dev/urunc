This section describes the high-level architecture of `urunc`, along with the
design choices and limitations.

## Overview

`urunc` is a container runtime designed to bridge the gap between traditional
unikernels and containerized environments. It enables seamless integration with
cloud-native architectures by leveraging familiar OCI (Open Container
Initiative) tools and methodologies. By acting as a unikernel container runtime
compatible with the Container Runtime Interface (CRI), `urunc` allows unikernels
to be managed like containers, opening up possibilities for lightweight,
secure, and high-performance application deployment.

In `urunc`, the user code runs inside a unikernel on top of a Virtual Machine
Monitor (VMM) or a sandbox monitor. As a result, `urunc` guarantees strong
isolation among the containers and inherits the enhanced security features of
unikernels, such as their small attack surface.

In the unikernel context a single-process application runs directly on top of a
Virtual Machine (VM) or a sandbox. At the same time, in the VM context, every
VM runs as a process. Subsequently, `urunc` combines these two characteristics
and treats the VM's process, which executes the unikernel that runs the
application, as the container's process. This way, `urunc` does not require any
auxiliary process running alongside the unikernel, maintaining as less overhead
as possible. Instead `urunc` directly manages the application running in the
unikernel through the VMM or the sandbox monitor. Moreover, `urunc` does not
require any modifications in the unikernel framework and hence all unikernel
frameworks and similar technologies can easily integrate with `urunc`.

## Execution flow

The process of starting a new unikernel container with `urunc`, starts at the
higher-level runtime (`containerd`) level:

- `Containerd` unpacks the image into a supported snapshotter (e.g. `devmapper`)
  and invokes `urunc`, as any other OCI runtime.
- `urunc` parses the image's rootfs and annotations, initiating the required
  setup procedures. In particular, it creates essential pipes for stdio, it
  creates the container's state file and runs the `prestart` hooks (if any).
- Subsequently, `urunc` spawns a new process within a distinct network
  namespace, stores its PID and invokes the `createRuntime` and
  `createContainer` hooks.
- When `Containerd` starts the container `urunc` configures any required
  resources such as block devices or  network interfaces and runs the
  `startContainer` hooks.
- Depending on the specified unikernel type and annotations, `urunc` selects the
  appropriate VMM or sandbox monitor (e.g.  Qemu, Solo5-spt) and boots the
  unikernel. The unikernel runs inside its own isolated environment, interacting
  with external systems through the namespaces and devices configured by
  `urunc`.
- Finally the unikernel is up and running as a container, and we can manage its
  lifecycle like any other container through `urunc` (e.g., stopping,
  restarting, or deleting the container).

## Image Format and Annotations

To support unikernels in a containerized environment, `urunc` requires specific
metadata embedded in OCI container images. These images must include the
unikernel binary, configuration and any other files required from the application
or the unikernel and the aforementioned metadata which dictate how the unikernel
should be run. The metadata can be passed to `urunc` either in the form of
[annotations](https://github.com/opencontainers/runtime-spec/blob/main/config.md#annotations)
or as a specific file in the container's rootfs. For a detailed explanation and
an up-to-date list of the currently supported annotations take a look at the
[packaging unikernels page](../package/#annotations).

Although `urunc`-formatted unikernel images are not designed to be executed by
other container runtimes, they can still be stored and distributed via generic
container registries, such as Docker Hub or Harbor. This ensures compatibility
with standard cloud-native workflows for building, shipping, and deploying
applications.
