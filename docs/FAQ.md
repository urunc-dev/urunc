# Frequently Asked Questions (FAQ) & Troubleshooting

This page addresses common questions and issues encountered while setting up and using `urunc`.

* * *

## General & Conceptual

### 1\. What is the difference between `urunc` and a standard container runtime?

Standard runtimes like `runc` use Linux namespaces and cgroups to isolate processes. `urunc` acts as a bridge that allows you to run Unikernels inside these same container environments by managing a Virtual Machine Monitor (VMM) like Firecracker or QEMU instead of a standard process.

### 2\. Can I run standard Docker images with `urunc`?

`urunc` is specifically designed for unikernel-based images (e.g., Unikraft, Rumprun). While you can use container tools like `nerdctl` or `docker` to pull and manage them, the underlying payload must be a unikernel.

### 3\. Which hypervisors does `urunc` support?

`urunc` currently supports Firecracker, QEMU, and Solo5 (hvt and spt tenders).

### 4\. Does `urunc` replace `containerd`?

No. `urunc` is a low-level runtime that integrates with `containerd` via a shim (`containerd-shim-urunc-v2`). `containerd` still handles the high-level management of the container lifecycle.

* * *

## Installation & Configuration

### 5\. Why do I need a block-based snapshotter?

`urunc` leverages block-based snapshots (like `devmapper` or `blockfile`) to treat a container's snapshot as a physical block device for the guest unikernel. This is essential for unikernels that require block storage for their root filesystem.

### 6\. Should I use `devmapper` or `blockfile`?

`devmapper` uses a thinpool for flexible management and is highly recommended. `blockfile` is simpler but lacks `ext2` support, making it incompatible with Rumprun unikernels.

### 7\. Why is my `devmapper` thinpool gone after a reboot?

The thinpool needs to be reloaded after every reboot. You should enable the `dm_reload.service` provided in the repository to automate this.

### 8\. How do I verify if `devmapper` is correctly configured in `containerd`?

Run the command `sudo ctr plugin ls | grep devmapper`. If the status is `ok`, the plugin is active.

### 9\. Where does `urunc` look for hypervisor binaries?

By default, it checks standard system paths. However, you must manually specify paths in your `urunc` configuration (usually in `config.toml`) for monitors like Firecracker or QEMU if they are installed in custom locations like `/opt/urunc/bin/`.

### 10\. Do I need `virtiofsd`?

`virtiofsd` is required if you intend to use `virtiofs` as an alternative to `9pfs` for sharing files between the host and the unikernel VM.

### 11\. Can I build `urunc` from source?

Yes, `urunc` can be built using Go (version 1.20.6 or later). Use the `make && sudo make install` command in the root of the repository.

* * *

## Debugging & Troubleshooting

### 12\. How do I see the internal logs of `urunc`?

You can enable debug logs by passing the `--debug` flag to `urunc`. A common trick is to create a bash wrapper that appends `--debug` to all execution calls and pipes the output to syslog.

### 13\. Can I `exec` into a running `urunc` unikernel?

Standard `docker exec` or `kubectl exec` will not work because there is no shell inside the unikernel VM. However, you can use the `cntr` tool to attach to the container namespace hosting the monitor process.

### 14\. What does `cntr` actually show me?

`cntr` allows you to see the environment _outside_ the VM but _inside_ the container namespace. You can see the VMM process (like `qemu` or `firecracker`) and access PTY devices like `/dev/console`.

### 15\. I get a "libseccomp-dev missing" error during installation. Why?

This is a required dependency for the `solo5-spt` monitor. Ensure you have the development headers for seccomp installed on your host.

### 16\. Why does Firecracker fail to start my Unikraft unikernel?

There are known compatibility issues between Unikraft and newer versions of Firecracker. It is recommended to use Firecracker v1.7.0 for the most stable experience with Unikraft.

### 17\. How do I check the console output of a unikernel?

Using `cntr`, you can inspect `/dev/console` within the container namespace to view the unikernel's boot logs and output.

### 18\. Why does my Rumprun unikernel fail on `blockfile`?

Rumprun requires `ext2` support, which the `blockfile` snapshotter does not currently support. Switch to `devmapper` for Rumprun unikernels.

### 19\. My container is "Up" but I can't reach the service. What's wrong?

Ensure you have installed the CNI plugins in `/opt/cni/bin`. Without these, the unikernel VM will not have the necessary networking interfaces to communicate.

### 20\. How do I update `urunc` to the latest version?

You can grab the latest static binaries for `urunc` and `containerd-shim-urunc-v2` directly from the GitHub releases page or the S3 bucket for the tip of the main branch.