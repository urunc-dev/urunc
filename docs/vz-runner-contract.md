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
| `--rootfs <path>` | no | Disk image attached as a virtio block device (`/dev/vda`) |
| `--share <host-path> <tag>` | no, repeatable | Host directory exported over virtiofs with the given tag. The backend uses tag `rootfs` for the root filesystem and `shared` for user-shared directories |
| `--mac <address>` | no | MAC address for the NAT network device (colon-separated hex). When omitted the runner may use a random address; the backend passes a deterministic one so the guest's DHCP lease can be located on the host by MAC |
| `--net-fd <n>` | no | Inherited file descriptor of a connected datagram socket; the runner backs the guest's single virtio-net device with it (`VZFileHandleNetworkDeviceAttachment`) **instead of** the NAT attachment. Used by orchestration layers to place guests on a user-mode gateway network, since the NAT attachment isolates guests from each other. The guest still sees exactly one interface |
| `--qmp <socket>` | no | Unix socket path on which the runner serves a minimal QMP endpoint (used for graceful shutdown) |

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
