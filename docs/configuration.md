`urunc` supports configuration through a TOML configuration file that allows you to customize various runtime behaviors including logging, timestamping, and monitor defaults. This document explains how to configure `urunc` using the configuration file.

## Configuration File Location

`urunc` looks for its configuration file at `/etc/urunc/config.toml` by default. The location can be overridden with the `URUNC_CONFIG_FILE` environment variable, for example `URUNC_CONFIG_FILE=/var/etc/urunc/config.toml`. If the file doesn't exist, `urunc` uses the [default values](#default-values). If it can not be read or decoded, every `urunc create` fails. The default values give Qemu no data files, which might fail Qemu execution.

One way to set the variable is to replace the `containerd-shim-urunc-v2` binary that containerd invokes with a small wrapper script that sets it and execs the real shim. For example, move the real shim aside (e.g. to `containerd-shim-urunc-v2.real`) and save the following as `/usr/local/bin/containerd-shim-urunc-v2`:

```bash
#!/bin/bash
URUNC_CONFIG_FILE=/some/path/config.toml exec /usr/local/bin/containerd-shim-urunc-v2.real "$@"
```

where `/some/path/config.toml` is the configuration file to use and `/usr/local/bin/containerd-shim-urunc-v2.real` is the actual shim binary.

## Configuration File Format

The configuration file uses the [TOML](https://toml.io/) format and is organized into several sections:

```toml
[runtime]
libcontainer = false
vAccel = false

[log]
level = "info"
syslog = false

[timestamps]
enabled = false
destination = "/var/log/urunc/timestamps.log"

[monitors.qemu]
default_memory_mb = 512
default_vcpus = 2
path = "/usr/bin/qemu-system-x86_64"
data_files = ["/usr/share/seabios/bios-256k.bin",
              "/usr/share/qemu/kvmvapic.bin",
              "/usr/share/qemu/multiboot_dma.bin",
              "/usr/share/qemu/linuxboot_dma.bin",
              "/usr/lib/ipxe/qemu/efi-virtio.rom"]
host_libs = ["/lib", "/lib64", "/usr/lib"]

[monitors.firecracker]
default_memory_mb = 256
default_vcpus = 1
path = "/usr/local/bin/firecracker"

[monitors.hvt]
default_memory_mb = 256
default_vcpus = 1

[monitors.spt]
default_memory_mb = 256
default_vcpus = 1

[extra_binaries.virtiofsd]
path = "/usr/libexec/virtiofsd"
options = "--sandbox none"
```

## Configuration Sections

### Runtime

The `[runtime]` section controls runtime-wide behavior.

| Field | Type | Default | Description |
|--------|------|---------|-------------|
| `libcontainer` | boolean | `false` | Use libcontainer to set up the monitor's execution environment |
| `vAccel` | boolean | `false` | Enable the support of vAccel through the `com.urunc.unikernel.vAccel` and `com.urunc.unikernel.RPCAddress` annotations |

**Notes**:
- the use of libcontainer to prepare the monitor execution environment is
  **experimental** and under active development. It is off by default.
- For the time being libcontainer-based setup does not support vAccel.
- The use of vAccel is **experimental** and requires a **specific deployment
  model**.
- The vAccel annotations MUST always be set from the operator of the cluster
  and in no case from user submitted images or pods.
- Annotations related to vAccel in the pod description or the image MUST be
  filtered out if they come from an untrusted source.
- When vAccel is enabled, `urunc` exposes the vsock devices to the sandbox
  monitor and, for Firecracker, makes only the vAccel agent's unix socket named
  by `com.urunc.unikernel.RPCAddress` visible to the monitor (read-only, at a
  fixed location inside the monitor's rootfs). The agent must already be
  listening on that socket when the container is created. See the [vAccel
  tutorial](tutorials/Running-vaccel-with-urunc.md) for the full setup.

**Example:**

```toml
[runtime]
libcontainer = false
vAccel = false
```

The value is resolved when the container is created and recorded in the
container's `state.json`, so `start`, `kill` and `delete` keep using the mode
the container was created with even if the configuration file changes in
between.

### Log Configuration

The `[log]` section controls logging behavior for `urunc`:

| Field | Type | Default | Description |
|--------|------|---------|-------------|
| `level` | string | `"info"` | Log level. Valid values: `"trace"`, `"debug"`, `"info"`, `"warn"`, `"error"`, `"fatal"`, `"panic"` |
| `syslog` | boolean | `false` | Enable syslog output in addition to stderr |

**Example:**

```toml
[log]
level = "debug"
syslog = true
```

**Note:** The effective log level is determined by both the configuration file and the command-line `--debug` flag.  
- If `--debug` is not specified, the log level from the configuration file is used.  
- If `--debug` is specified, the effective log level is the more verbose of:
  - the configuration file’s log level, and  
  - `"debug"` (the level implied by the flag).  

For example:  
- Config: `"warn"`, CLI: `--debug` → effective level = `"debug"`  
- Config: `"trace"`, CLI: `--debug` → effective level = `"trace"`  
- Config: `"error"`, no `--debug` → effective level = `"error"` 

### Timestamps Configuration

The `[timestamps]` section controls timestamp logging for performance monitoring:

| Field | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable timestamp logging for performance metrics |
| `destination` | string | `"/var/log/urunc/timestamps.log"` | File path where timestamps will be written |

**Example:**

```toml
[timestamps]
enabled = true
destination = "/tmp/urunc-timestamps.log"
```

When enabled, `urunc` will log performance timestamps to help with debugging and optimization.

### Monitor Configuration

The `[monitors]` section allows you to configure default settings for different
VM/Sandbox monitors. Each monitor is configured as a subsection with its own default
values.

#### Supported monitors and their respective subsection names

- [QEMU/KVM](./hypervisor-support#qemu) - `qemu`
- [Firecracker](./hypervisor-support#firecracker) - `firecracker`
- [Cloud-Hypervisor](./hypervisor-support#cloud-hypervisor) - `cloud-hypervisor`
- [Solo5-hvt](./hypervisor-support#solo5-hvt) - `hvt` - Solo5 hvt (KVM-based tender)
- [Solo5-spt](./hypervisor-support#solo5-spt) - `spt` - Solo5 spt (Seccomp-based tender)
- [Hyperlight](./hypervisor-support#hyperlight) - `hyperlight-unikraft`

#### Monitor Fields

Each monitor subsection supports the following fields:

| Field | Type | Default | Description |
|--------|------|---------|-------------|
| `default_memory_mb` | integer | `256` | Default memory allocation in megabytes |
| `default_vcpus` | integer | `1` | Default number of virtual CPUs |
| `path` | string | (empty) | Optional custom path to the monitor binary. If not specified, urunc will search for the binary in PATH |
| `data_files` | list of strings | (empty) | Optional host paths of the monitor's data files, or of a single directory with them. Currently only used by the `qemu` monitor |
| `vhost` | boolean | `false` | Optional: enable `vhost-net` for the monitor's network device, to improve network performance. Currently only honored by the `qemu` monitor |
| `socket_path` | string | (empty) | Optional path for the monitor's control socket. If not set, the monitor runs without a control socket |
| `host_libs` | list of strings | (empty) | Optional host paths with the shared libraries the monitor needs. Each one is mounted read-only at the same path in the monitor's execution environment |

The `path`, `socket_path`, `host_libs` and `data_files` fields must hold
absolute and clean paths, e.g. without `..` or a trailing `/`. This applies to
every monitor in the file, even one that no container uses. Otherwise, every
`urunc create` fails, also for the containers that `urunc` hands to `runc`.

`urunc` creates a minimal execution environment for each monitor, which by
default contains no host libraries. Therefore, we strongly recommend
statically linked monitors, like the ones in the [monitors-build
releases](https://github.com/urunc-dev/monitors-build/releases), since they do
not need any shared libraries from the host. Other files that a monitor needs
from the host are added with the following fields:

- `host_libs`: The shared libraries of a dynamically linked monitor, like the
  Qemu package of a distribution or a Solo5 tender built from source, and its
  dynamic loader. `urunc` does not know how a monitor was built, so it does not
  add any of them by default. Every path must exist on the host and be either a
  regular file or a directory, otherwise the container fails to start. A path
  can not be, contain or be inside anything that `urunc` adds itself in the
  monitor's execution environment, like `/proc`, `/dev`, `/tmp`, the monitor
  binary, its control socket or, for Qemu, its data files in `/usr/share/qemu`.
  For example, `/` can never be listed, while `/usr/share` can not be listed for
  Qemu with `data_files`. A path can also not contain or be inside another
  listed one, including the ones in the `host_libs` of an extra binary that the
  container uses, like `virtiofsd`, but the same path can be listed in both. A
  directory is mounted with everything inside it, so list only the directories
  with the libraries. For instance, the monitor can connect to any host socket
  inside a listed directory, even though the mount is read-only.
- `data_files`: Qemu's data files (e.g. BIOS and option ROMs), either as files
  or as a single directory with all of them. It applies only to Qemu, and the
  other monitors ignore it. Each listed file is mounted under its own name in
  `/usr/share/qemu`, where Qemu looks for its data files, so two files can not
  have the same name. A listed directory is mounted as a whole at the same
  place, so it must be the only entry. Every path must exist on the host,
  otherwise the container fails to start.

A good starting point to find the libraries needed by a dynamically linked
monitor is `ldd <monitor binary>`. For example, for a `solo5-spt` built from
source on Ubuntu 24.04 (x86_64):

```console
$ ldd /usr/local/bin/solo5-spt
	linux-vdso.so.1 (0x00007ffc329ed000)
	libseccomp.so.2 => /lib/x86_64-linux-gnu/libseccomp.so.2 (0x000077920b922000)
	libc.so.6 => /lib/x86_64-linux-gnu/libc.so.6 (0x000077920b600000)
	/lib64/ld-linux-x86-64.so.2 (0x000077920b953000)
```

```toml
[monitors.spt]
host_libs = ["/lib64/ld-linux-x86-64.so.2",
        "/lib/x86_64-linux-gnu/libc.so.6",
        "/lib/x86_64-linux-gnu/libseccomp.so.2"]
```

When listing `host_libs`, keep in mind that:

- The `/etc/ld.so.cache` of the host is not available to the monitor, so the
  libraries must be in the default search paths of the dynamic loader or in the
  RUNPATH of the binary.
- Files are preferred over directories. A symlink inside a listed directory
  that points outside of it (e.g. `/lib64/ld-linux-x86-64.so.2` or the links to
  `/etc/alternatives` on Debian and Ubuntu) will be broken inside the monitor's
  execution environment unless its target is also listed.
- Every listed path is a separate mount at each container start, so collecting
  the necessary files in a directory is the optimal setup.
- `ldd` does not show the libraries loaded at run time with `dlopen()`, like the
  modules of Qemu (e.g. in `/usr/lib/x86_64-linux-gnu/qemu` on Debian and
  Ubuntu), and its output can change after an upgrade of the monitor.
- A dynamically linked `virtiofsd` needs its libraries listed too. So does a
  dynamically linked `urunc` in libcontainer mode, since it runs in the
  monitor's execution environment.

The data files that Qemu needs depend on the guest and its devices, and Qemu
names the one it misses (e.g. `failed to find romfile "efi-virtio.rom"`). For
example, the Qemu package of Ubuntu 24.04 (x86_64) needs the following files to
boot a Unikraft unikernel with a network interface:

```toml
[monitors.qemu]
data_files = ["/usr/share/seabios/bios-256k.bin",
              "/usr/share/qemu/kvmvapic.bin",
              "/usr/share/qemu/multiboot_dma.bin",
              "/usr/lib/ipxe/qemu/efi-virtio.rom"]
```

A Linux guest needs `linuxboot_dma.bin` instead of `multiboot_dma.bin`. If it
is missing, Qemu only warns `Could not open option rom 'linuxboot_dma.bin'` and
the guest does not start. Both of them can be listed to boot either kind of
guest.

When all the data files are in a single directory, like in the [monitors-build
releases](https://github.com/urunc-dev/monitors-build/releases), that directory
can be listed on its own instead:

```toml
[monitors.qemu]
data_files = ["/opt/urunc/share/qemu/qemu"]
```

This is not possible with the Qemu package of Debian and Ubuntu, whose data
files are spread across `/usr/share/qemu`, `/usr/share/seabios` and
`/usr/lib/ipxe/qemu`.

The `socket_path` field applies to the monitors that expose a control socket:
Firecracker (its API socket), Qemu (a QMP socket) and Cloud Hypervisor (its REST
API socket). It has no effect on the other monitors. The control socket is
opt-in: it exists only when `socket_path` is set. If it is not set, the
monitor runs with no control socket at all; an operator can leave it unset if
they do not need the socket, or to keep a smaller attack surface.

When it is set, the monitor creates the socket inside its own (pivoted) rootfs.
`urunc` creates the directory of `socket_path` inside that rootfs after it drops
privileges to the monitor's user, so the path is subject to two constraints:

- Its parent directory must be one the monitor's user can create and write. A
  directory that only `root` can write does not work for a non-root monitor.
- It must not sit over an existing file or directory. `urunc` creates the
  directory of `socket_path`; that fails cleanly if a file already exists at one
  of the directories in the path. The monitor then binds the socket at
  `socket_path`; that fails if a file already exists at `socket_path` itself.

`urunc` removes the socket when the container is stopped (on a terminating
signal) and when it is deleted, so a restart that reuses the same `socket_path`
does not find a stale socket.

**Example:**

```toml
[monitors.qemu]
default_memory_mb = 1024
default_vcpus = 4
path = "/usr/local/bin/qemu-system-x86_64"
data_files = ["/usr/local/share/qemu"]
vhost = false

[monitors.firecracker]
default_memory_mb = 512
default_vcpus = 2
path = "/opt/firecracker/firecracker"
socket_path = "/run/urunc/fc.sock"
```

### Extra binaries Configuration

The `[extra_binaries]` section allows users to configure default settings for
different extra binaries to be included in the monitor's container. Each
extra binary is configured as a subsection with its own default values.

#### Supported extra binaries

- `virtiofsd` - vhost-user virtio-fs device backend written in Rust

#### Extra binaries Fields

Each extra binary subsection supports the following fields:

| Field | Type | Default | Description |
|--------|------|---------|-------------|
| `path` | string | (empty) | Optional custom path to the extra binary. If not specified, urunc will search for the binary in PATH |
| `options` | string | (empty) | Optional custom cli options for the extra binary |
| `host_libs` | list of strings | (empty) | Optional host paths with the shared libraries the extra binary needs, as in the [monitor's `host_libs` field](#monitor-fields). They are mounted only when the extra binary is used |

The `path` and `host_libs` fields must hold absolute and clean paths, as for the
monitors.

Specifically for `virtiofsd` the default values are the following:

- `path`: `/usr/libexec/virtiofsd`
- `options`: `--cache always --sandbox none`

> **NOTE**: Currently, in the case of libcontainer the option `--sandbox` should be set to none, due to missing capabilities inside the monitor's execution environment.

**Example:**

```toml
[extra_binaries.virtiofsd]
path = "/usr/local/bin/virtiofsd"
options = "--sandbox none --cache always"
```

## Creating the Configuration File

To create a configuration file, you can:

1. **Create the directory structure:**

    ```bash
    sudo mkdir -p /etc/urunc
    ```

2. **Create the configuration file:**

   ```bash
   sudo tee /etc/urunc/config.toml > /dev/null <<EOF
   [log]
   level = "info"
   syslog = false

   [timestamps]
   enabled = false
   destination = "/var/log/urunc/timestamps.log"

   [monitors.qemu]
   default_memory_mb = 512
   default_vcpus = 2

   [monitors.firecracker]
   default_memory_mb = 256
   default_vcpus = 1

   [monitors.hvt]
   default_memory_mb = 256
   default_vcpus = 1

   [monitors.spt]
   default_memory_mb = 256
   default_vcpus = 1
   EOF
   ```

   For Qemu, also add its `data_files` to `[monitors.qemu]`, as described in
   [Monitor Configuration](#monitor-configuration).

## Configuration Validation

`urunc` will validate the configuration file when it starts. If the configuration file:

- **Does not exist**: `urunc` uses default values and logs a warning
- **Can not be read or decoded** (e.g. no read permission, a syntax error, a table defined twice or a field of the wrong type): every `urunc create` fails with the error, also for the containers that `urunc` hands to `runc`, since the default values would silently replace every field of the file
- **Contains an invalid path**: a `path`, `socket_path`, `host_libs` or `data_files` field of a monitor, or a `path` or `host_libs` field of an extra binary, that is not absolute and clean makes every `urunc create` fail in the same way
- **Contains other invalid values**: `urunc` will either use default values for invalid fields, e.g. for a zero `default_vcpus` or an unknown log `level`, or fail to start if critical errors are found

## Default Values

If no configuration file is provided, `urunc` uses these default values:

```toml
[log]
level = "info"
syslog = false

[timestamps]
enabled = false
destination = "/var/log/urunc/timestamps.log"

[monitors.qemu]
default_memory_mb = 256
default_vcpus = 1
# path is not set by default - urunc will search in PATH
# data_files is not set by default - Qemu gets no data files

[monitors.firecracker]
default_memory_mb = 256
default_vcpus = 1
# path is not set by default - urunc will search in PATH

[monitors.hvt]
default_memory_mb = 256
default_vcpus = 1
# path is not set by default - urunc will search in PATH

[monitors.spt]
default_memory_mb = 256
default_vcpus = 1
# path is not set by default - urunc will search in PATH

[monitors.cloud-hypervisor]
default_memory_mb = 256
default_vcpus = 1
# path is not set by default - urunc will search in PATH

[monitors.hyperlight-unikraft]
# default_memory_mb is not applied: without a limit, hluk sizes the guest itself
default_vcpus = 1
# path is not set by default - urunc will search in PATH
```

## Notes

- The configuration file is only fully loaded during `urunc create`. The configuration values are then stored as Annotations in the `state.json` file inside the respective container's bundle. For subsequent urunc commands (such as `start`, `kill`, etc.), configuration values are loaded from the `state.json` annotations. In that way, all urunc configuration values except logging configuration and timestamping (see below) remain the same throughout the specific container lifecycle.
- The configuration file is partially loaded every time urunc is invoked to parse the logging configuration and timestamping fields. This way, the user has fine-grained control over the logging level and whether to redirect urunc logs to syslog. Similarly, the user can enable and disable timestamping.
