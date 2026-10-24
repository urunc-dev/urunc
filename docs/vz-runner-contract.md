# vz-runner: external VMM helper contract

The darwin Vz backend (`pkg/unikontainers/hypervisors/vz_darwin.go`) does not
link against Apple's Virtualization.framework directly. Instead it launches an
external helper binary, `vz-runner`, which owns the `VZVirtualMachine`
lifecycle. Any binary that satisfies the contract below can act as the runner.

## Discovery

The backend resolves the runner as the file named `vz-runner` located **in the
same directory as the running executable** (`os.Executable()`). Install the
runner next to the binary that embeds urunc.

## Invocation

```
vz-runner --kernel <path> [options]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--kernel <path>` | yes | Uncompressed ARM64 Image format kernel (VZLinuxBootLoader) |
| `--mem <MB>` | yes | Guest memory size in MiB |
| `--cpus <N>` | yes | Number of vCPUs |
| `--initrd <path>` | no | Initial ramdisk |
| `--cmdline <string>` | no | Kernel command line |
| `--rootfs <path>` | no | Disk image attached as a virtio block device (`/dev/vda`). The legacy single root disk; it carries no serial |
| `--disk <path>` | no, repeatable | Disk image attached read-write as a virtio block device, after the `--rootfs` disk if any. See [Disks and serials](#disks-and-serials) |
| `--disk-ro <path>` | no, repeatable | As `--disk`, attached read-only (`VZDiskImageStorageDeviceAttachment(readOnly: true)`) |
| `--share <host-path> <tag>` | no, repeatable | Host directory exported over virtiofs with the given tag. The backend uses tag `rootfs` for the root filesystem and `shared` for user-shared directories |
| `--share-ro <host-path> <tag>` | no, repeatable | As `--share`, exported read-only. The mode is carried by the flag, so the arguments keep the same shape |
| `--mac <address>` | no | MAC address for the NAT network device (colon-separated hex). When omitted the runner may use a random address; the backend passes a deterministic one so the guest's DHCP lease can be located on the host by MAC |
| `--net-fd <n>` | no | Inherited file descriptor of a connected datagram socket; the runner backs the guest's single virtio-net device with it (`VZFileHandleNetworkDeviceAttachment`) **instead of** the NAT attachment. Used by orchestration layers to place guests on a user-mode gateway network, since the NAT attachment isolates guests from each other. The guest still sees exactly one interface |
| `--qmp <socket>` | no | Unix socket path on which the runner serves a minimal QMP endpoint (used for graceful shutdown) |

## Disks and serials

`--disk` and `--disk-ro` may be given any number of times, in any
interleaving. The runner appends them to `storageDevices` in command-line
order, after the `--rootfs` disk (or the EFI boot disk) if there is one, and
sets each one's `blockDeviceIdentifier` to `disk<N>`. N is the 0-based
position of the disk among the `--disk` and `--disk-ro` flags only; the
`--rootfs` disk is not counted and gets no serial. The guest reads the serial
from `/sys/block/vd*/serial` and never relies on `/dev/vdX` order. Serials are
at most 20 bytes.

The backend emits `--rootfs` only for the legacy single disk
(`ExecArgs.BlockDevPath` with `ExecArgs.BlockDevs` empty), and that disk
replaces the root share. Disks in `ExecArgs.BlockDevs` go out over
`--disk`/`--disk-ro` in order and leave the root share (`--share` or
`--share-ro` with tag `fs0` unless set) in place. For example, a read-only
lower followed by a writable upper:

```
vz-runner ... --share-ro <root> fs0 --disk-ro lower.ext4 --disk upper.ext4
```

gives `lower.ext4` serial `disk0` and `upper.ext4` serial `disk1`.

When checkpointing, the runner clones only the writable disks and records the
read-only ones by path.

## Runtime behavior

- The guest serial console must be attached to the runner's **stdin/stdout**;
  diagnostic messages go to stderr.
- The runner exits with the VM: a clean guest shutdown must end the process
  with exit code 0.
- **SIGTERM** must trigger a graceful VM stop (equivalent to
  `VZVirtualMachine.requestStop`).
- The runner is expected to attach a NAT network device
  (`VZNATNetworkDeviceAttachment`) when networking is requested via the kernel
  command line (`ip=dhcp`).

## Signing requirements

The runner must be signed with the `com.apple.security.virtualization`
entitlement. With a valid Apple Development or Developer ID identity the
entitlement is honored with SIP enabled; ad-hoc signed runners additionally
require AMFI to be disabled. NAT networking needs no further entitlements
(`com.apple.vm.networking` is only required for bridged mode).
