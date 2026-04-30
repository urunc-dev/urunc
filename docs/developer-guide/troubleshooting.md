---
title: Troubleshooting urunc
description: "Common issues encountered when running urunc and how to resolve them"
---

# Troubleshooting

This page collects the most common problems users hit when installing,
configuring, or running `urunc`, along with the steps to diagnose and resolve
them. If you run into an issue not covered here, please open one on the
[issue tracker](https://github.com/urunc-dev/urunc/issues) so this guide can
grow with the community.

For a deeper interactive debugging workflow (attaching to the container
namespace, propagating logs to syslog, etc.), see the
[Debugging guide](debugging.md).

## Collecting information before debugging

Before digging into a specific failure, gather a baseline. Most issues become
much faster to diagnose with the following on hand:

```bash
urunc --version
runc --version
containerd --version
uname -a
```

Enable verbose logs by passing `--debug` to `urunc`. The simplest way to do
this without re-configuring `containerd` is the wrapper script described in
[Debugging with Logs](debugging.md#debugging-with-logs). With the wrapper in
place, `urunc` events are visible via:

```bash
sudo journalctl -t urunc -f
```

When filing a bug, attach:

- the output of the commands above,
- the relevant `urunc` syslog excerpt,
- the OCI image reference (or its annotations),
- the `containerd` snapshotter in use (`devmapper`, `blockfile`, `overlayfs`).

## Installation issues

### `containerd` does not pick up the `urunc` runtime

**Symptom:** `nerdctl run --runtime io.containerd.urunc.v2 ...` fails with
`failed to start shim: exec: "containerd-shim-urunc-v2": executable file not
found in $PATH`.

**Cause:** the `urunc` shim binary is not installed on a directory that is in
`containerd`'s `PATH`, or `containerd` was not restarted after installation.

**Fix:**

```bash
which containerd-shim-urunc-v2
sudo install -m 755 containerd-shim-urunc-v2 /usr/local/bin/
sudo systemctl restart containerd
```

Verify the runtime is registered by inspecting `/etc/containerd/config.toml`
for a `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.urunc]`
section. See the [Installation guide](../installation.md) for the full
configuration block.

### `unknown runtime specified: io.containerd.urunc.v2`

**Cause:** the `containerd` configuration is using the v1 plugin schema, or
the configuration file was edited but `containerd` was not reloaded.

**Fix:** make sure you are using `containerd` 1.6 or newer with the CRI
plugin enabled, then run `sudo systemctl restart containerd`. Confirm with
`sudo ctr plugins ls | grep cri`.

### `urunc` cannot find a snapshotter that supports block devices

**Symptom:** the container fails at create time with an error mentioning
`devmapper` or `blockfile`.

**Cause:** unikernel images frequently ship a raw block device as a layer.
The default `overlayfs` snapshotter cannot expose that block device to the
monitor.

**Fix:** install and enable either the
[`devmapper`](https://docs.docker.com/storage/storagedriver/device-mapper-driver/)
or
[`blockfile`](https://github.com/containerd/containerd/blob/main/docs/snapshotters/blockfile.md)
snapshotter, then pass `--snapshotter devmapper` (or `blockfile`) to
`nerdctl`/`ctr`. See the
[Installation guide](../installation.md#configure-a-block-device-snapshotter).

## Image and packaging issues

### `urunc` annotations are missing from the image

**Symptom:** `urunc` exits with `failed to retrieve unikernel type` or a
similar message at create time.

**Cause:** the OCI image was not built with the `urunc`-specific
annotations (`com.urunc.unikernel.unikernelType`,
`com.urunc.unikernel.binary`, etc.).

**Fix:** rebuild the image with `bima` or follow the
[Building/Packaging guide](../package/index.md). The annotations live on the
image manifest — you can inspect them with:

```bash
nerdctl image inspect <image> --mode native | jq '.[0].Manifest.annotations'
```

### Wrong unikernel binary architecture

**Symptom:** the monitor starts but immediately exits with `Exec format
error` or `kvm: unhandled exit`.

**Cause:** the unikernel binary in the image does not match the host CPU
architecture (e.g., an `aarch64` unikernel pulled on an `x86_64` host).

**Fix:** build or pull the image variant that matches `uname -m`. For
multi-arch repositories, ensure the manifest list contains an entry for the
host architecture.

## Runtime and monitor issues

### `KVM: permission denied` when launching qemu/firecracker

**Symptom:** the monitor logs `Could not access KVM kernel module: Permission
denied`.

**Cause:** the user (or the containerd shim, when running rootless) does not
have access to `/dev/kvm`.

**Fix:**

```bash
ls -l /dev/kvm
sudo usermod -aG kvm "$USER"
# log out and back in for the group to take effect
```

If you are running in a virtualized environment (cloud VM, nested
virtualization), confirm that nested KVM is enabled on the host:

```bash
cat /sys/module/kvm_intel/parameters/nested   # or kvm_amd
```

### Container exits immediately with no output

**Symptom:** `nerdctl run` returns instantly, exit code is non-zero, and
there are no application logs.

**Cause:** the unikernel boots and exits before its stdout is connected, or
the monitor binary is missing.

**Fix:**

1. Run with `-it` and the
   [debug image](debugging.md#using-cntr-with-urunc) to confirm the
   container namespace is set up correctly.
2. Enable debug logs (`--debug`) and check `journalctl -t urunc` for the
   exact monitor invocation.
3. Verify the configured monitor is installed and on `PATH`:

   ```bash
   command -v qemu-system-x86_64 firecracker solo5-hvt
   ```

### Stuck on `waiting for vsock` / `waiting for tap device`

**Cause:** the host kernel is missing the `vhost_vsock` or `tun` modules, or
the container's network namespace was torn down before the monitor finished
attaching.

**Fix:**

```bash
sudo modprobe vhost_vsock tun
lsmod | grep -E 'vhost_vsock|^tun'
```

If the issue only appears under load, see
[Network and TAP device leaks](#network-and-tap-device-leaks) below.

## Network and TAP device leaks

**Symptom:** after running and killing many `urunc` containers, the host
accumulates orphan `tap0_<id>` interfaces visible under `ip link`, and new
containers eventually fail to start.

**Cause:** a goroutine that joined the sandbox network namespace did not lock
itself to the OS thread, so the namespace was switched back on the wrong
goroutine and the cleanup ran in the host namespace.

**Fix:** this is the regression addressed in
[`fix(kill): lock OS thread around sandbox netns join to prevent TAP
leak`](https://github.com/urunc-dev/urunc/commit/80f887e). Upgrade to a
release that includes the fix, then clean up any leftover interfaces:

```bash
ip -o link show type tap | awk -F': ' '/tap0_/ {print $2}' | xargs -r -n1 sudo ip link del
```

If you observe a new variant of the leak, capture the output of `ip link`
and `ip netns list` immediately after the failure and attach it to a bug
report.

### MTU mismatch between host and unikernel

**Symptom:** TCP connections inside the unikernel hang on large payloads but
small pings work.

**Cause:** the TAP device MTU is not propagated to the monitor's command
line, so the guest negotiates a larger MTU than the host bridge supports.

**Fix:** upgrade to a release containing
[`fix(network): get MTU from tap device and set it in qemu and clh
args`](https://github.com/urunc-dev/urunc/commit/a04eed8). Confirm with:

```bash
ip link show <tap-iface> | awk '/mtu/ {print $5}'
```

The monitor command line (visible with `--debug`) should contain a matching
`mtu=` argument.

## Kubernetes / CRI issues

### Pods stay in `ContainerCreating`

**Cause:** `RuntimeClass` is not registered, or the node is missing the
`urunc` shim or a required snapshotter.

**Fix:**

```bash
kubectl get runtimeclass urunc -o yaml
kubectl describe pod <pod> | tail -20
```

Make sure the `RuntimeClass` `handler` matches the runtime name configured
in `containerd` (typically `urunc`), and that the node selector — if any —
points at hosts where `urunc` is installed. See the
[Kubernetes tutorial](../tutorials/How-to-urunc-on-k8s.md).

### `failed to reserve sandbox name` after node reboot

**Cause:** stale shim state from a previous `containerd` run.

**Fix:**

```bash
sudo systemctl stop containerd
sudo rm -rf /run/containerd/io.containerd.runtime.v2.task/k8s.io/<sandbox-id>
sudo systemctl start containerd
```

Only remove entries that correspond to sandboxes already evicted by the
kubelet.

## Getting more help

If the steps above do not resolve the problem:

1. Re-run the failing command with `--debug` and capture
   `journalctl -t urunc` for the same window.
2. Search existing
   [issues](https://github.com/urunc-dev/urunc/issues) and
   [discussions](https://github.com/urunc-dev/urunc/discussions).
3. Open a new issue with the information from
   [Collecting information before debugging](#collecting-information-before-debugging),
   the exact image reference, and a minimal reproduction.
