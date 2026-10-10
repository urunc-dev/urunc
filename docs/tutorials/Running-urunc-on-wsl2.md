# Running `urunc` on WSL2

In this tutorial, we describe how to set up and run `urunc` on
**Windows Subsystem for Linux 2 (WSL2)**.

## Prerequisites

Before proceeding, make sure you have a working WSL2 instance with
Ubuntu installed. You can follow the official
[Microsoft WSL documentation](https://learn.microsoft.com/en-us/windows/wsl/install)
to set this up.

### Verify KVM support

Some of `urunc` supported monitors require KVM for
hardware-accelerated virtualization. On WSL2, KVM is available only if
nested virtualization is enabled on your Windows host.

Verify that `/dev/kvm` exists:
```bash
ls -la /dev/kvm
```

If the device is missing, enable nested virtualization by creating or
editing the `.wslconfig` file at `%USERPROFILE%\.wslconfig` on your
Windows host:
```ini
[wsl2]
nestedVirtualization=true
```

Then restart WSL from PowerShell:
```powershell
wsl --shutdown
```

After restarting, verify KVM is available:
```bash
lsmod | grep kvm
```

You should see `kvm_intel` or `kvm_amd` in the output.

## Install `urunc`

Follow the standard [installation guide](../installation.md) to build
and install `urunc` and the containerd shim.

## Snapshotter configuration

The default WSL2 kernel does not load the `dm_thin_pool` kernel module,
which means the `devmapper` snapshotter will not work out of the box.

Verify this by running:
```bash
lsmod | grep dm_thin
```

If the output is empty, you can use the `overlayfs` snapshotter instead,
which works without any additional configuration on WSL2.

When running containers, use the `--snapshotter overlayfs` flag:
```bash
sudo nerdctl run --snapshotter overlayfs --runtime io.containerd.urunc.v2 ...
```

## Using `nerdctl` or Docker

You can use either `nerdctl` or Docker to run containers with `urunc`.
An installation of Docker inside the WSL2 Ubuntu instance works fine.
Alternatively, you can use standalone `nerdctl` with a manually
configured containerd.

To install `nerdctl`, follow the official
[nerdctl documentation](https://github.com/containerd/nerdctl#install).

## Running a unikernel

Once everything is set up, you can run a unikernel container. For
example, to run the Unikraft nginx image with QEMU:

```bash
sudo nerdctl run --rm --snapshotter overlayfs \
  --runtime io.containerd.urunc.v2 \
  harbor.nbfc.io/nubificus/urunc/nginx-qemu-unikraft-initrd:latest
```

To verify the unikernel is running, check its IP address from the
`nerdctl` output and send a request:
```bash
curl http://<unikernel-ip>
```

You should see the default nginx welcome page.
