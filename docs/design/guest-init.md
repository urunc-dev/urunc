---
layout: default
title: "Guest init"
description: "Why the guest side of a generic container boot is one static C binary"
---

# Guest init of a generic container boot

## Decision

The guest side of a generic container boot is one static C binary, `urunit`.
It runs as the early init, as the supervisor (PID 1) of the container
application, and as the exec agent. It is built with musl in an Alpine
container.

The binary lives in [NOFireAI/urunit](https://github.com/NOFireAI/urunit).
urunc pins a commit of it in `packaging/container-initrd/urunit.pin`, and
`build-urunit.sh` builds that commit for the boot initrd.

The boot initrd used to carry four pieces that did this work: the `/init`
and `/vz-init` shell scripts, a BusyBox for the scripts, the prebuilt
`urunit` supervisor and the Go `urunit-agent`. The single binary replaces all
four.

The language was chosen against three options:

- Extend the Go agent. The agent would stay a Go binary, and the supervisor
  would still need the C `urunit` or a Go rewrite of it. The Go agent alone
  is already larger than the whole new binary.
- Rust. There is no existing code to reuse, and a Rust build adds a musl
  target and toolchain to every build.
- C. The supervisor, the reaping, the controlling terminal setup, the
  configuration parser, the block mounts and the reboot path already exist
  in C in [urunit](https://github.com/NOFireAI/urunit). Each job of the early
  init is a syscall. The agent is an fd-based event loop, and it needs no
  runtime.

### Criteria

Each criterion of the evaluation, answered for C, Go and Rust.

- Static size. C wins by a factor of ten against the Go agent alone (the
  table below). A Rust binary with the same features would land between the
  two. The Rust standard library and the `libc` bindings add to the size, and
  the code still has to be written.
- Boot latency. The shell init spawned a BusyBox process for every `mount`,
  `mkdir`, `cp` and `chmod`, then exec'd `switch_root`, then `urunit`, then
  the Go agent, which pays the Go runtime start. Measured on arm64 in the
  podman VM, 1000 fork+exec rounds of an empty static program take 0.50 s
  for C and 1.51 s for Go, so the Go runtime start alone costs about 1 ms
  more per start. The C binary makes the same steps with system calls in one
  process, and the init roles become the supervisor with no further exec.
  The savings are in the order of milliseconds. They are small next to the
  kernel boot, so latency does not decide the language. It does not count
  against C either.
- PID 1 duties. Reaping, `PR_SET_CHILD_SUBREAPER`, signal forwarding, the
  controlling terminal (`TIOCSCTTY`) and the reboot or power-off call are all
  system calls with no runtime in the way. The existing `urunit` already does
  them in C, including the fixes of `fix/controlling-tty`. A Go PID 1 would
  have to deal with the runtime threads and with `fork` in a multi-threaded
  process.
- Mounts and `switch_root`. `mount(2)` for proc, sysfs, devtmpfs, virtiofs,
  overlay, tmpfs and `binfmt_misc`, `MS_MOVE`, and the `switch_root` steps
  (`chdir`, `MS_MOVE` onto `/`, `chroot`) are plain calls in all three
  languages. The recorded-sysops tests make them checkable in C.
- Network setup. `ip=` is set up by the kernel. The only guest step is the
  default route for a gateway outside the subnet (`URUNIT_DEFROUTE`), one
  `ioctl(SIOCADDRT)` that already exists in `urunit` in C. The netlink and
  tc fallbacks for kernels without XFRM_USER or u32 run on the host, in
  `pkg/network`, and do not reach the guest.
- Agent concurrency. The agent has several exec sessions on several
  connections, a pty relay that retries on `EIO`, non-blocking asynchronous
  logging, close-on-exec accepted connections, and a virtio-serial transport
  that works without vsock. One epoll loop with per-connection queues covers
  this in C. Go runs each session as a goroutine on the threads of its
  runtime, which is simpler to write, and it is what the Go agent does
  today. The cost is the runtime and the size. C needed more care for the
  queues and for the `fork` and `exec` window.
- Reuse. The supervisor, its configuration parser, the block mounts and the
  reboot path are C code that works. Go and Rust would rewrite them.
- Cross-build from macOS. C builds in an Alpine container with
  `podman run --platform linux/<arch>`, the same way for amd64 and arm64.
  Go cross-compiles with `GOOS` and `GOARCH` and needs no container. Rust
  needs the musl target and a linker for each arch. C is the one that needs
  the container, and that is the price of this choice.
- Test strategy. The early init takes a `struct sysops` so that the C tests
  record every call and compare the sequence with the shell scripts. The
  agent is tested through the real `pkg/agentproto` client in Go, over a unix
  socket, which is language independent and gives the same coverage for all
  three options.

C is preferred in the task, and nothing in the list argues against it. The
size, the reuse and the PID 1 duties argue for it. Go would be easier for the
agent and costs the size and a second binary for PID 1. Rust has no
advantage here that offsets the new code and the toolchain.

The sizes below are measured values.

| Component                              | Size (bytes)          |
|----------------------------------------|-----------------------|
| Go `urunit-agent`, arm64               | 2,424,994             |
| Go `urunit-agent`, amd64               | 2,474,146             |
| `urunit` (supervisor), prebuilt        | 132,896               |
| BusyBox (the scripts' shell and tools) | about 1,100,000       |
| `urunit` single binary, arm64          | 202,656               |
| `urunit` single binary, amd64          | 128,600               |

The single binary is stripped, static and built with musl. It replaces the
Go agent, the prebuilt `urunit` and BusyBox. On arm64 those three come to
about 3.7 MB. The single binary is 202,656 bytes on arm64.

The binary is cross-built from macOS with
`podman run --platform linux/<arch> alpine:3.20`. An emulated amd64 build
takes about 8 seconds. Go still needs the C supervisor or a rewrite of it, so
a Go agent would not remove the C code path.

The Go agent speaks the `pkg/agentproto` wire format. The C agent keeps that
format byte for byte, and a Go test checks it against the real binary.

## Where it lives

urunit already has the supervisor, its CI, the published images and the
FreeBSD port. It is also used outside a container boot: in the urunit images
and in FreeBSD guests. Keeping the guest code there avoids a second copy that
drifts from the first. On FreeBSD urunit builds without the early init and the
agent, which are Linux only.

The two repositories share two formats: the agentproto frames and the
`urunit.conf` records. urunc owns both. `pkg/agentproto` and
`BuildUrunitConfig` are the host side, and `tests/urunit` checks the pinned
binary against them. A change to either format needs a urunit commit and a
bump of the pin in the same urunc PR. `urunit.conf` is a list of
NUL-terminated records, the format of urunc-dev/urunc#1111 and
nubificus/urunit#17. urunit tells it from the legacy lines by its first
record. agentproto has no version yet.

## What the binary does

The role is picked at start. The `URUNIT_MODE` environment variable wins over
the name the binary runs under. Without either, the binary runs as the
supervisor.

| Role        | Selected by                                  | Job                                                                                                      |
|-------------|----------------------------------------------|----------------------------------------------------------------------------------------------------------|
| init        | `/init` or `URUNIT_MODE=init`                | Early init for `rdinit=/init`. The root comes from `root=`, `rootfstype=` and `rootflags=`. Any failure ends the boot. |
| vz-init     | `/vz-init` or `URUNIT_MODE=vz-init`          | Early init for `rdinit=/vz-init` on Vz. The root is the virtiofs tag `rootfs`. Failures are logged. |
| agent       | `urunit-agent` or `URUNIT_MODE=agent`        | The exec agent. It serves `pkg/agentproto` sessions until it is killed.                                  |
| supervisor  | any other name, or `URUNIT_MODE=supervise`  | Supervisor (PID 1) of the application. It starts the agent when `/run/urunc/urunit` is executable.       |

The init roles mount `/proc`, `/sys` and `/dev`, set the clock, and register
Rosetta when asked. They then mount the root, install the files, mount the
exports, stage the binary, move the mounts, and switch to the new root. The
init roles become the supervisor in the same process.

The order of the middle steps follows the script each role replaces, and it
matters when an export covers `/tmp`, `/etc` or `/run`:

- `init` stages the binary, mounts the `/urunc-mounts` exports, then installs
  the network files, as `container-init` did.
- `vz-init` exports `/urunc-env`, sets the root `/tmp` to 01777, installs the
  network files, mounts the exports, and stages the binary last, as `vz-init`
  did. The chmod and the files land in the root and not in a host share, and
  an export at `/run` does not hide the staged binary.

The init roles read these files from the initrd. A step runs only when its
file is present.

| File           | `init` (Linux hosts, hull with a block image)                         | `vz-init` (hull on Vz and hvi)                          |
|----------------|---------------------------------------------------|-----------------------------------------|
| `/urunc-epoch` | Sets the clock from the file                      | Same                                    |
| `/urunc-rosetta` | Mounts the `rosetta` share and registers Rosetta | Same                                    |
| `/urunc-overlay` | Mounts a tmpfs overlay over the root            | Not read; the overlay is the default    |
| `/urunc-rootfs-direct` | Not read                                  | Mounts the root directly, not as an overlay |
| `/urunc-mounts` | Mounts each virtiofs export under `/newroot`   | Same                                    |
| `/urunc-resolv.conf`, `/urunc-hosts`, `/urunc-hostname` | Installed into the new root | Same, or the resolver lines of `/proc/net/pnp` |
| `/urunc-env`   | Ignored                                           | Exported, one `KEY=VALUE` per line      |
| `/urunc-cmd`   | Replaces the command, one argument per line       | Same                                    |
| `/urunit.conf` | Copied to `/run/urunc/urunit.conf`, and `URUNIT_CONFIG` points at it | Ignored |

The supervisor reads the application configuration from `URUNIT_CONFIG`. On
a container boot, that is `/run/urunc/urunit.conf`. The file holds the
environment, user, working directory and block mounts of the container.

The Rosetta step mounts the virtiofs share `rosetta` at `/mnt/rosetta` and
mounts `binfmt_misc`. It then writes this 189-byte record to
`/proc/sys/fs/binfmt_misc/register`:

```
:rosetta:M::\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02\x00\x3e\x00:\xff\xff\xff\xff\xff\xfe\xfe\x00\xff\xff\xff\xff\xff\xff\xff\xff\xfe\xff\xff\xff:/mnt/rosetta/rosetta:OCF
```

The kernel decodes the `\x` escapes. The C source keeps the backslashes
doubled, so the bytes written are the literal text. The same record was
produced by the shell scripts before this change.

### Agent transports

The agent serves one of these transports. `URUNIT_AGENT_TRANSPORT` selects it.

- Unset: vsock port 1024 with `VMADDR_CID_ANY`. When vsock is not available,
  the agent probes the virtio-serial port `io.urunc.agent.0`. A setup failure
  is retried every 200 ms.
- `vsock` or `virtio-port`: pins one transport.
- `unix:<path>`: listens on a unix socket. Many connections are served at the
  same time. This is for tests.
- `fd:<n>`: serves an inherited descriptor with virtio-port semantics. This is
  for tests.

An unknown value is logged and treated as unset.

On the virtio port, a read of 0 bytes means the host peer is gone. The agent
kills that peer's sessions and keeps the descriptor open. It waits 100 ms and
serves the next peer on the same descriptor with a fresh frame parser.

The agent runs one epoll loop. The loop waits on the listener, the
connections, the pty masters and pipes of the sessions, and a signalfd for
`SIGCHLD`. Each connection has a bounded output queue.

### Log output

The agent writes its log to the console through a non-blocking descriptor. A
write that would block drops the line and counts it. The next line written
reports how many lines were dropped. A full console cannot stall the loop.

## Boot contract

The host side is unchanged. A container boot passes these to the monitor:

- `rdinit=/init` for a container boot on a Linux host, and for hull with a
  root disk image. The command line keeps the root words of the monitor. For a shared
  filesystem, that is `root=fs0 rw rootfstype=virtiofs`. For a block device,
  it is `root=/dev/vda rw rootfstype=<fs>`. The `/init` mounts the root from
  these words, since the kernel does not mount it for an initramfs.
- `rdinit=/vz-init` for hull on Vz and on hvi. hull passes no `root=` or
  `rootfstype=`. The init takes the root from the `rootfs` virtiofs tag, or,
  with `HULL_ROOTFS_MODE=overlay-block`, from the read-only disk
  `/urunc-rootfs-lower` names (`block:<serial>:<fstype>`), with the overlay
  upper on the disk `/urunc-rootfs-upper` names (`block:<serial>`, or the
  older `block` and `virtiofs`). Disks are found by their virtio-blk serial.
  With a disk lower or a persistent upper, every failure ends the boot.
- `panic=-1`. With it, a failed init panics the kernel, the guest reboots, and
  the monitor exits. A failed container boot then fails instead of hanging.
- `console=` for the console the monitor provides.
- `ip=` and `URUNIT_DEFROUTE=1` for networking. The second word is set when
  the gateway is outside the subnet, and the supervisor then adds the default
  route with `SIOCADDRT`.
- `URUNIT_POWEROFF=1` on Cloud Hypervisor. The supervisor powers the guest off
  instead of rebooting it, since Cloud Hypervisor restarts the VM on a reset.

For a container boot the environment is not on the command line. It is in the
`urunit.conf` file appended to the initrd.

Both modes mount `cgroup2` on `/sys/fs/cgroup` in the new root, and write an
`/etc/hosts` with the loopback names when neither the image nor hull has one,
as hull-assets' `vz-init` does.

### Deviations from the scripts

These are the places where the binary does not reproduce `container-init`,
`vz-init` and the old `urunit` step by step.

- A root that cannot be mounted ends `vz-init`. The script ran
  `exec /busybox sh` and left a rescue shell on the console. PID 1 now
  exits and the kernel panics. hull does not pass `panic=-1` on this path,
  so the VM stays in the panic. The same holds for a failed overlay whose
  direct-mount fallback fails too. The init mode already ended the boot on
  every failure.
- `vz-init` mounts `/proc`, `/sys` and `/dev` once and moves them into the
  new root. The script mounted them again there. The init mode moves `/sys`
  as well as `/dev` and `/proc`. `container-init` unmounted `/proc` and left
  `/sys` to the old `urunit`.
- `vz-init` unmounts `binfmt_misc` after it registers Rosetta, as
  `container-init` does. The script left it mounted.
- `/proc` and `/sys` get `nosuid`, `nodev` and `noexec`, `devpts` gets
  `nosuid` and `noexec`, and `/dev/shm` gets `nosuid` and `nodev`. The
  scripts mounted them with no flags.
- The supervisor forks the agent as a child instead of executing
  `/run/urunc/urunit-agent`. `urunit-agent` is still staged as a symlink to
  `urunit`, for tools that look for it. The agent runs in its own process
  group and gets `SIGTERM` when the application exits.
- The init mode mounts a `tmpfs` with mode 1777 on `/newroot/tmp` when it
  stages `urunit.conf`. The old `urunit` did this in `mount_special_fs()`
  whenever it had a `URUNIT_CONFIG`. The mount comes before `/urunc-mounts`,
  so an export that covers `/tmp` wins. `vz-init` has no `urunit.conf`, as
  before, and keeps the rootfs `/tmp` with its mode set to 1777.

### Known gaps on the hull side

These gaps predate this change. The binary handles the files that hull writes.
The differences from the scripts it replaces are in the section above.

- hull writes `/urunc-cmd` and `/urunc-env` on both container-boot paths.
  `/urunc-env` is read only in `vz-init` mode. On the `/init` path the
  environment is not exported, as before.
- The `rdinit=/init` path that hull uses for a root disk image
  (`rootfsDiskImage`) does not pass `root=` or `rootfstype=` on the command
  line. It also does not append a `urunit.conf`. The init mode fails on the
  missing root words, so this path does not boot today. This change does not
  alter it.

## Testing

The C unit tests live in urunit, under `src/tests`, and run with
`make check` in its CI, with glibc and with musl. Each file covers one area.

- `test_main.c`: the mode lookup, with the environment override and the base
  name of `argv[0]`.
- `test_proto.c`: the frame header, the JSON of `OpenRequest`, the resize and
  signal messages, and the error and exit messages.
- `test_config.c`: the `urunit.conf` parser.
- `test_init.c`: both init sequences, run through a recorded `sysops`. The
  test checks each step, its order, and the messages of the failure paths.
- `test_agent.c`: the user and environment parsing, the exit codes, the output
  queue, and the frame queue of a connection once it is closed.

The Go tests in `tests/urunit` drive the pinned binary with the
`pkg/agentproto` client, over a unix socket and an inherited descriptor. They
run the binary `URUNIT_BIN` names. `make test_urunit` builds the pinned
binary and runs them, in a `golang:1.26-alpine` container on hosts that are
not Linux. Tests that switch users or check the owner of a pty need root.

The initrd was booted on these hosts:

- macOS (arm64), brig with hull on Vz through `rdinit=/vz-init`: an
  interactive shell with a pty, exit codes, SIGTERM, 4 MB of output and six
  concurrent `hull exec` sessions.
- Linux (x86_64, KVM), urunc with qemu and with cloud-hypervisor, alpine and
  ubuntu images, through `rdinit=/init`: the same exec checks, `/tmp`, and an
  environment value with a new line, a variable named like the `UEE` marker
  and the working directory, all through the NUL-terminated records.

Rosetta (x86_64 images on macOS) and hull's `rdinit=/init` path are not
tested yet.

## Rollback

`INITRD_LAYOUT=legacy` builds the previous layout with `/init`, `/vz-init`,
`/busybox`, `/urunit` and `/urunit-agent`. The scripts and `cmd/urunit-agent`
stay in the tree until the new layout is validated everywhere. Rolling back
needs no change on the host side. `URUNIT` has to name a urunit that reads
NUL-terminated records with nothing before them (NOFireAI/urunit `de324cf` or
later). One from `da5e62f` on reads them as lines, so a value with a new line
breaks. An older one runs the command without its environment, user and
working directory.

## Provenance

The guest code is a stack of branches in NOFireAI/urunit, on top of
urunit's `main`:

1. `stack/1-nul-records`: the NUL-terminated configuration records, then
   behind a `URUNIT1` record.
2. `stack/2-tty-reap`: the glibc build fix, the controlling tty and the
   reaping fixes.
3. `stack/3-poweroff`: `URUNIT_POWEROFF` for cloud-hypervisor.
4. `stack/4-single-binary`: the supervisor split, the agentproto codec, the
   early init, the exec agent and the CI.
5. `stack/5-overlay-block`: the `vz-init` root from a read-only and a
   writable disk, for hull's overlay-block mode.
6. `stack/6-init-markers`: the `/init` root from the same markers.
7. `stack/7-stop-signals`: stop signals forwarded to the application, and
   `URUNIT_MODE=signal`.
8. `stack/8-sync-before-agent`: the supervisor syncs before it stops the
   agent.
9. `stack/9-nul-without-magic`: NUL-terminated records without the `URUNIT1`
   record.

`urunit.pin` names the tip of the last one. One change affects the agent:
`reap()` returns as soon as the application is reaped. It syncs, sends
`SIGTERM` to the agent and does not wait for it, and the reboot that follows
ends the agent. The sync comes first because the agent's sessions end with
it: urunc reads the end of the stop signal's session as the guest going
down.
