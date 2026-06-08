`urunc` supports configuration through a TOML configuration file that allows you to customize various runtime behaviors including logging, timestamping, and monitor defaults. This document explains how to configure `urunc` using the configuration file.

## Configuration File Location

`urunc` looks for its configuration file at `/etc/urunc/config.toml`. If the file doesn't exist or contains invalid configuration, `urunc` will use sensible defaults and continue to operate normally.

## Configuration File Format

The configuration file uses the [TOML](https://toml.io/) format and is organized into several sections:

```toml
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

### Log Configuration

The `[log]` section controls logging behavior for `urunc`:

| Option | Type | Default | Description |
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

| Option | Type | Default | Description |
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
- [Solo5-hvt](./hypervisor-support#solo5-hvt) - `hvt` - Solo5 hvt (KVM-based tender)
- [Solo5-spt](./hypervisor-support#solo5-spt) - `spt` - Solo5 spt (Seccomp-based tender)

#### Monitor Options

Each monitor subsection supports the following options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `default_memory_mb` | integer | `256` | Default memory allocation in megabytes |
| `default_vcpus` | integer | `1` | Default number of virtual CPUs |
| `path` | string | (empty) | Optional custom path to the monitor binary. If not specified, urunc will search for the binary in PATH |
| `data_path` | string | (empty) | Optional custom path for the monitor's data file directory |

Since Qemu is the only currently supported monitor which requires extra data to
boot a VM, `urunc` will first check `/usr/local/share` and then `/usr/share` for
Qemu's data files.

**Example:**

```toml
[monitors.qemu]
default_memory_mb = 1024
default_vcpus = 4
path = "/usr/local/bin/qemu-system-x86_64"
data_path = "/usr/local/share/"

[monitors.firecracker]
default_memory_mb = 512
default_vcpus = 2
path = "/opt/firecracker/firecracker"
```

### Extra binaries Configuration

The `[extra_binaries]` section allows users to configure default settings for
different extra binaries to be included in the monitor's container. Each
extra binary is configured as a subsection with its own default values.

#### Supported extra binaries

- `virtiofsd` - vhost-user virtio-fs device backend written in Rust

#### Extra binaries Options

Each extra binary subsection supports the following options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `path` | string | (empty) | Optional custom path to the extra binary. If not specified, urunc will search for the binary in PATH |
| `options` | string | (empty) | Optional custom cli options for the extra binary |

Specifically for `virtiofsd` the default values are the following:

- `path`: `/usr/libexec/virtiofsd`
- `options`: `--cache always --sandbox none`

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

## Configuration Validation

`urunc` will validate the configuration file when it starts. If the configuration file:

- **Does not exist**: `urunc` uses default values and logs a warning
- **Contains syntax errors**: `urunc` uses default values and logs a warning about the parsing error
- **Contains invalid values**: `urunc` will either use default values for invalid fields or fail to start if critical errors are found

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
```

## Notes

- The configuration file is only fully loaded during `urunc create`. The configuration options' values are then stored as Annotations in the `state.json` file inside the respective container's bundle. For subsequent urunc commands (such as `start`, `kill`, etc.), configuration options are loaded from the `state.json` annotations. In that way, all urunc configuration values except logging configuration and timestamping (see below) remain the same throughout the specific container lifecycle.
- The configuration file is partially loaded every time urunc is invoked to parse the logging configuration and timestamping options. This way, the user has fine-grained control over the logging level and whether to redirect urunc logs to syslog. Similarly, the user can enable and disable timestamping.
