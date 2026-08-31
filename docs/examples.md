# Building Unikernel Images for urunc

This guide demonstrates how to build Unikernel applications and package them as OCI container images so they can be seamlessly deployed over `urunc`.

## Prerequisites

To follow these examples, you must have the following tools available on your host system:
- **[urunc](https://urunc.io/):** The unikernel container runtime.
- **[containerd](https://containerd.io/):** The container daemon for managing the images.
- **[kraftkit](https://unikraft.org/docs/cli):** The Unikraft CLI, used to build the raw unikernel binaries.
- **Docker:** We use Docker along with the [nubificus/bunny](https://github.com/nubificus/bunny) BuildKit frontend to package the binaries.

## The Building Process

The recommended, robust way to build unikernel images for `urunc` is a two-step process. Rather than writing a monolithic Dockerfile that executes `kraft build` internally (which can cause BuildKit-in-BuildKit nesting issues), we separate the compilation from the packaging:

1. **Compile the Unikernel:** Use standard Unikraft tooling (e.g., `kraft build`) to compile your application into a unikernel binary and generate its corresponding root filesystem (`initrd`).
2. **Package with Bunny:** Create a `bunnyfile` that points to the compiled binaries. When you run `docker build -f bunnyfile`, the `bunny` frontend kicks in. It takes the binary and rootfs, packages them into a standard OCI container image, and automatically injects all the necessary `com.urunc.unikernel.*` annotations so that `urunc` knows exactly how to execute it.

## Storage Support

`urunc` supports standard container storage patterns. When you package your image using `bunny`, you specify a `rootfs` (typically a `cpio` archive) which `urunc` will mount at runtime as the base filesystem. 

If your application requires persistent data (e.g., a database like Redis), you can also mount additional persistent volumes via standard Kubernetes or `nerdctl` volume mounts. These will be passed through to the unikernel.

## End-to-End Examples

We provide three complete examples of this process:

- [Nginx](../examples/nginx/README.md)
- [Redis](../examples/redis/README.md)
- [Httpreply](../examples/httpreply/README.md)
