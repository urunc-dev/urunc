# Running existing containers in `urunc` with Linux

While Linux is not a unikernel framework, it remains the most widely used kernel
in cloud infrastructure. As a result, the majority of applications and services
are built to run on Linux. At the same time, Linux has a very highly
configurable build system, and as proven by
[Lupine](https://dl.acm.org/doi/10.1145/3342195.3387526), we can build tailored
Linux kernels optimized for running a single application.

With this goal in mind, this guide walks through the steps required to take an
existing container image and execute it on top of `urunc` as a Linux virtual
machine (VM).

Overall, we need to do the following:

1. Build or reuse a Linux kernel.
2. (Optional but recommended) Build or fetch an init process.
3. Prepare the final image by appending the Linux kernel (and init) and set up
   `urunc` annotations.

## Linux kernel

The main requirement for running existing containers on top of `urunc` is a
Linux kernel. From `urunc`'s side there are no specific kernel configuration
options required, but since Linux will run on virtual machine monitors like
[Qemu](https://qemu.org) or
[Firecracker](https://github.com/firecracker-microvm/firecracker), the kernel
should be configured with the necessary drivers (e.g., virtio devices).

To simplify this, you can find
[here](https://gist.github.com/cmainas/223e1525496dd2c8e08dbf8bab41df80) a
sample x86 kernel configuration based on [Linux
v6.14](https://github.com/torvalds/linux/tree/v6.14), which builds a minimal
kernel around 13 MiB in size. Note that this configuration excludes features
like cgroups and certain system calls, so additional customization may be
required depending on your application.

Alternatively, prebuilt kernels are available via the following container images:

- `harbor.nbfc.io/nubificus/urunc/linux-kernel-qemu:v6.14`
- `harbor.nbfc.io/nubificus/urunc/linux-kernel-firecracker:v6.14`

Each image contains the Linux kernel binary at `/kernel`.

## Init process

After booting, the Linux kernel hands control to the init process, the first
user-space program. This process acts as the root of the process tree and must
remain running. If it exits, the kernel will panic.

In single-application environments, the application itself can serve as init.
However, this is not always reliable:

- If the application exits, the system halts.
- CLI argument handling may be incorrect: Linux does not natively support
  multi-word arguments via kernel boot parameters. Each space-separated word is
treated as a separate argument.
- A few necessary operations (such as mount /proc, set default route) might be
  required before executing the application.
- The Linux kernel can only handle a limited number of characters in its boot
  parameters. If too many or very long environment variables are passed, the
  boot process will fail once that limit is exceeded. 

Especially, for CLI argument handing, `urunc` follows a simple convention. All
multi-word CLI arguments are wrapped in single quotes and the init process (or
application) is expected to reconstruct them properly.

For all the above reasons, we strongly recommend using a dedicated init process.
We provide [urunit](https://github.com/nubificus/urunit#); a lightweight init
designed specifically for `urunc`. It performs the following actions:

1. Sets default route through eth0. This is necessary when we deploy
   the container in a Kubernetes cluster, where there is a high chance that
   the gateway of the container might be in a different subnet than the IP.
   As a result, Linux kernel will fail to set the gateway.
2. Groups multi-word arguments correctly.
3. Mounts `/proc` and `/sys`.
4. Reads and sets the environment variables for the application execution
5. Reads and sets the uid,gid and working directory for the application execution
6. Acts as a reaper, cleaning up zombie processes.

To pass the necessary information to
[urunit](https://github.com/nubificus/urunit), `urunc` uses a configuration
file that passes to the VM with the following format:

```
UES
/* list of environment variables */
UEE
UCS
UID:<uid>
GID:<gid>
WD:<working_dir>
UCE
```

In order to minimize the dependencies for the Linux kernel running as guest,
`urunc` attaches this configuration file as an initrd of the VM and sets the
`retain_initrd` kernel boot parameter. In that way,
[urunit](https://github.com/nubificus/urunit) will be able to read the
configuration file from `/sys/firmware/initrd`. In case the guest is configured
to boot with a user-specified initrd, then `urunc` will archive in the cpio
format the configuration file and concatenate it with the user-provided initrd.
The Linux kernel is smart enough to handle concatenated initrd and hence the
configuration file will appear in the guest's rootfs.

As a result, in case [urunit](https://github.com/nubificus/urunit) is used we
strongly recommend to configure the Linux kernel setting the following
configuration options:

- Support for initrd `CONFIG_BLK_DEV_INITRD`
- Support for sysfs `CONFIG_SYSFS`
- Support for procfs `CONFIG_PROC_FS`

You can obtain [urunit](https://github.com/nubificus/urunit) in two ways:

- Fetch a static binary from [urunit's release
  page](https://github.com/nubificus/urunit/releases).
  Via the container image: `harbor.nbfc.io/nubificus/urunit:latest`,
  with the binary located at `/urunit`.

## Preparing the image

To differentiate traditional containers from unikernels, `urunc` uses specific
[annotations](../image-building#annotations). Therefore, to run a container with
a Linux kernel on `urunc`, these annotations must be configured, and the Linux
kernel must be included in the container image’s root filesystem. To simplify
this process, we will use [bunny](https://github.com/nubificus/bunny).

Another important aspect is preparing the root filesystem (rootfs). Since we're
booting a full Linux virtual machine, a proper rootfs must be provided. There
are three main ways to do this:

1. Using directly the rootfs of the container's image (requires either a block based snapshotter or 9pfs/Virtiofs).
2. Creating a block image out of a container's image rootfs.
3. Creating a initrd.

### Using directly the container's rootfs

The simplest way to boot an existing container with a Linux kernel on `urunc`
is to reuse the container’s rootfs. This is possible either through shared-fs
between the host and the Linux VM or by using devmapper as the snapshotter. In
the latter case containerd's devmapper snapshotter will create a snapshot of
the container's image in the form of a block image and `urunc` can then
directly attach this block image to the VM.

To set up devmapper as a snapshotter please refer to the [installation
guide](../installation#setup-thinpool-devmapper).

#### Preparing the container image.

In this case preparing the container image involves two key steps:

1. Append the Linux kernel binary to the container image.
2. Set the appropriate `urunc` annotations.

These tasks can be easily automated with [bunny](https://github.com/nubificus/bunny).

Let's use as an example the `redis:alpine` container image using the Linux
kernel from `harbor.nbfc.io/nubificus/urunc/linux-kernel-qemu:v6.14`. The
respective `bunnyfile` would look like:

```yaml
#syntax=harbor.nbfc.io/nubificus/bunny:latest
version: v0.1

platforms:
  framework: linux
  monitor: qemu
  architecture: x86

rootfs:
  from: redis:alpine
  type: raw

kernel:
  from: harbor.nbfc.io/nubificus/urunc/linux-kernel-qemu:v6.14
  path: /kernel

cmdline: "/usr/local/bin/redis-server"
```

We can build the container with:

```bash
docker build -f bunnyfile -t redis/apline/linux/qemu:latest .
```

Alternatively, if the Linux kernel was built locally, we can update the kernel
section of the bunnyfile to reference the local binary:

```yaml
kernel:
  from: local
  path: bzImage
```

By default, this setup will run redis-server as the init process.  To include
[urunit](https://github.com/nubificus/urunit) in the redis:alpine image, we can
use the following Containerfile:

```
FROM harbor.nbfc.io/nubificus/urunit:latest AS init

FROM redis:alpine

COPY --from=init /urunit /urunit
```

> **NOTE**: We are working towards enabling the addition of extra files from the
> `bunnyfile`. We will update this page once this feature is supported.

After building the above container, make sure to specify it in the `from` field
of rootfs in `bunnyfile`:

```yaml
rootfs:
  from: redis/urunit:alpine
  type: raw
```

At last we need to modify the `cmdline` section of `bunnyfile` to execute
[urunit](https://github.com/nubificus/urunit):

```yaml
cmdline: "/urunit /usr/local/bin/redis-server"
```

#### Running the container

Unfortunately, Docker requires additional setup to work with the devmapper
snapshotter. Therefore, in order to run the container using Docker, we can
only use shared-fs.

```bash
docker run --rm -it --runtime "io.containerd.urunc.v2" redis/apline/linux/qemu:latest
```

Let's find the IP of the container:
```console
$ docker inspect <CONTAINER ID> | grep IPAddress
            "SecondaryIPAddresses": null,
            "IPAddress": "172.17.0.2",
                    "IPAddress": "172.17.0.2",
```

and we should be able to ping it:

```bash
ping -c 3 172.17.0.2
```

Alternatively, if we want to use devmapper as the snapshotter, we can use
[nerdctl](https://github.com/containerd/nerdctl), which integrates seamlessly
with containerd and supports devmapper out of the box.

First, transfer the container image from Docker’s image store to containerd:
```bash
docker save redis/apline/linux/qemu:latest | nerdctl load
```

With the image now available in containerd, we’re ready to run the container
using urunc and the devmapper snapshotter:

```bash
nerdctl run --rm -it --snapshotter devmapper --runtime "io.containerd.urunc.v2" redis/apline/linux/qemu:latest
```

Let's find the IP of the container:
```console
$ nerdctl inspect <CONTAINER ID> | grep IPAddress
            "IPAddress": "10.4.0.2",
                    "IPAddress": "10.4.0.2",
                    "IPAddress": "172.16.1.2",
```

and we should be able to ping it:

```bash
ping -c 3 10.4.0.2
```

### Using a block image

If we have a block image that can be used as a rootfs, we can instruct `urunc`
to pass this block image as a rootfs.

#### Preparing the container image.

To prepare the container image we will need to first create block image. For
that purpose, we will use `nginx:alpine` image and we will choose to run it on
top of Firecracker. We can create the block image with the following steps:

```bash
dd if=/dev/zero of=rootfs.ext2 bs=1 count=0 seek=60M
mkfs.ext2 rootfs.ext2
mkdir tmp_mnt
mount rootfs.ext2 tmp_mnt
docker export $(docker create nginx:alpine) -o nginx_alpine.tar
tar -xf nginx_alpine.tar -C tmp_mnt
wget -O tmp_mnt/urunit https://github.com/nubificus/urunit/releases/download/v0.1.0/urunit_x86_64 # If we want urunit as init
chmod +x tmp_mnt/urunit # If we want urunit as init
umount tmp_mnt
```

Now we have a block image, `rootfs.ext2`, generated from the `nginx:alpine`
container and including [urunit latest
release](https://github.com/nubificus/urunit/releases/tag/v0.1.0). To
package everything together, we will use a file with Containerfile-like syntax,
just to demonstrate how to manually define the required annotations for `urunc`:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest
FROM scratch

COPY vmlinux /kernel
COPY nginx_rootfs.ext2 /rootfs.ext2

LABEL "com.urunc.unikernel.binary"="/kernel"
LABEL "com.urunc.unikernel.cmdline"="/urunit /usr/sbin/nginx -g 'daemon off;error_log stderr debug;"
LABEL "com.urunc.unikernel.unikernelType"="linux"
LABEL "com.urunc.unikernel.block"="/rootfs.ext2"
LABEL "com.urunc.unikernel.blkMntPoint"="/"
LABEL "com.urunc.unikernel.hypervisor"="firecracker"
```

We can build the container with:

```bash
docker build -f Containerfile -t nginx/apline/linux/firecracker:latest .
```

#### Running the container

We can run the container with the following command:

```bash
docker run --rm -it --runtime "io.containerd.urunc.v2" nginx/apline/linux/firecracker:latest
```

Let's find the IP of the container:
```console
$ docker inspect <CONTAINER ID> | grep IPAddress
            "SecondaryIPAddresses": null,
            "IPAddress": "172.17.0.2",
                    "IPAddress": "172.17.0.2",
```

and we should be able to curl it:

```console
$ curl 172.17.0.2
<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
<style>
html { color-scheme: light dark; }
body { width: 35em; margin: 0 auto;
font-family: Tahoma, Verdana, Arial, sans-serif; }
</style>
</head>
<body>
<h1>Welcome to nginx!</h1>
<p>If you see this page, the nginx web server is successfully installed and
working. Further configuration is required.</p>

<p>For online documentation and support please refer to
<a href="http://nginx.org/">nginx.org</a>.<br/>
Commercial support is available at
<a href="http://nginx.com/">nginx.com</a>.</p>

<p><em>Thank you for using nginx.</em></p>
</body>
</html>
```

### Using initrd as a rootfs

Similarly to the previous approach, we can create an initrd instead of a block
image to use as the root filesystem. To demonstrate this, we will use the
`traefik/whoami` container image as an example.

#### Preparing the container image.

First let's create the initrd:

```bash
mkdir tmp_rootfs
docker export $(docker create traefik/whoami) | tar -C tmp_rootfs/ -xvf -
wget -O tmp_rootfs/urunit https://github.com/nubificus/urunit/releases/download/v0.1.0/urunit_x86_64 # If we want urunit as init
chmod +x tmp_rootfs/urunit
cd tmp_rootfs
find . | cpio -H newc -o > ../rootfs.initrd
```

> **NOTE**: We are working towards enabling the creation of the initrd directly
> from [bunny](https://github.com/nubificus/bunny). We will update this page
> once this feature is supported.

Now we have an initrd `rootfs.initrd` generated from `traefik/whoami` and with
[urunit](https://github.com/nubificus/urunit) that we got from its [latest
release](https://github.com/nubificus/urunit/releases/tag/v0.1.0).  In order to
pack everything together, we can use the following `bunnyfile`:

```
#syntax=harbor.nbfc.io/nubificus/bunny:latest
version: v0.1

platforms:
  framework: linux
  monitor: firecracker
  architecture: x86

rootfs:
  from: local
  type: initrd
  path: rootfs.initrd

kernel:
  from: harbor.nbfc.io/nubificus/urunc/linux-kernel-firecracker:v6.14
  path: /kernel

cmdline: "/urunit /whoami"
```

We can build the container with:

```bash
docker build -f bunnyfile -t traefik/whoami/linux/firecracker:latest .
```

#### Running the container

We can run the container with the following command:

```bash
docker run --rm -it --runtime "io.containerd.urunc.v2" traefik/whoami/linux/firecracker:latest
```

Let's find the IP of the container:
```console
$ docker inspect <CONTAINER ID> | grep IPAddress
            "SecondaryIPAddresses": null,
            "IPAddress": "172.17.0.2",
                    "IPAddress": "172.17.0.2",
```

and we should be able to curl it:

```console
$ curl 172.17.0.2
Hostname: urunc
IP: 127.0.0.1
IP: 172.17.0.2
RemoteAddr: 172.17.0.1:42684
GET / HTTP/1.1
Host: 172.17.0.2
User-Agent: curl/7.68.0
Accept: */*
```
