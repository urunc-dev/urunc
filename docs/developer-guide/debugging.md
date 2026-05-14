---
title: Debugging urunc Containers
description: "Guide for debugging urunc"
---

## Debugging urunc Containers with cntr

This guide explains how to attach to a running `urunc` container using
[`cntr`](https://github.com/Mic92/cntr), in order to inspect its environment
and use additional debugging tools.

`cntr` overlays an alternative root filesystem on top of the container namespace,
allowing access to utilities such as `ls`, `ps`, that are not present
in the original environment.

## Using cntr with urunc 

### Prerequisites

Install cntr:

```bash
cargo install cntr
```

If you don't have Rust/Cargo installed:

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
source ~/.cargo/env
cargo install cntr
```

### Steps

1. **Start a urunc container:**

    ```bash
    sudo docker run -d --name urunc-debug --runtime io.containerd.urunc.v2 -it \
      harbor.nbfc.io/nubificus/urunc/dbg/ubuntu:dltme /bin/bash
    ```

2. **Get the container ID:**

    ```bash
    $ sudo docker ps -a
    CONTAINER ID   IMAGE                                             COMMAND       CREATED         STATUS         PORTS     NAMES
    56b93fbd7332   harbor.nbfc.io/nubificus/urunc/dbg/ubuntu:dltme   "/bin/bash"   3 seconds ago   Up 3 seconds             urunc-debug
    ```

3. **Attach with cntr:**

    ```bash
    sudo cntr attach 56b93fbd7332
    ```

You now have an interactive shell with access to debugging tools!

### Output:

```bash
$ sudo cntr attach 56b93fbd7332
root@host:/var/lib/cntr#

# List PTY devices
root@host:/var/lib/cntr# ls -la /dev/pts
drwxr-xr-x 2 root root      0 Nov  3 09:07 .
crw------- 1 root tty  136, 0 Nov  3 09:07 0
crw------- 1 root tty  136, 1 Nov  3 09:11 1
crw-rw-rw- 1 root root   5, 2 Nov  3 09:11 ptmx

# Check console device
root@host:/var/lib/cntr# ls -la /dev/console
-rw-rw-rw- 1 root root 0 Nov  3 09:07 /dev/console

# View processes 
root@host:/var/lib/cntr# ps aux | grep qemu

# Inspect container filesystem
root@host:/var/lib/cntr# ls -la 
```
### What `cntr` Enables

Using `cntr` with a urunc container gives:

- Working PTY devices (`/dev/pts`, `/dev/ptmx`, `/dev/console`)
- A debugging environment with common tools (e.g., `ls`, `ps`, `strace`)
- Visibility into the container namespace where the monitor process (qemu/firecracker/solo5) runs

> **Note:** `cntr` does **not** enter the unikernel VM — it only provides access to the container namespace hosting the monitor.

## Debugging with Logs

To enable debugging logs, we need to pass the `--debug` flag when calling `urunc`. Also, to facilitate easier
debugging, when the `debug` flag is true all logs are propagated to the syslog.

An easy way to achieve this is to create a Bash wrapper for `urunc`:

```bash
sudo mv /usr/local/bin/urunc /usr/local/bin/urunc.default
sudo tee /usr/local/bin/urunc > /dev/null <<'EOT'
#!/usr/bin/env bash
exec /usr/local/bin/urunc.default --debug "$@"
EOT
sudo chmod +x /usr/local/bin/urunc
```