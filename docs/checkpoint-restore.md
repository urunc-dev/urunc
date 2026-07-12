# Checkpoint and Restore

`urunc` supports checkpointing a running container and restoring it later,
possibly in a different container instance. Since every `urunc` container is
a microVM, checkpoint/restore is implemented as a **VM snapshot**: the VMM
pauses the guest and serializes its full state (device model and guest
memory) to disk, and a fresh VMM process later resumes the guest from that
state. Unlike `runc`, no CRIU is involved; the snapshot is taken by the VMM
itself through its control API.

## Supported VMMs

| VMM | Checkpoint/Restore | Mechanism |
| --- | ------------------ | --------- |
| Firecracker | ✅ | `PUT /snapshot/create` / `PUT /snapshot/load` over the API socket |
| Cloud Hypervisor | ✅ | `vm.snapshot` / `--restore` + `vm.resume` over the API socket |
| QEMU | ❌ (planned, via QMP `migrate`) | — |
| Solo5-hvt / Solo5-spt / Hedge | ❌ (no snapshot support in the monitor) | — |

To make this possible, `urunc` always starts Firecracker and Cloud
Hypervisor with their control API enabled on a unix socket
(`/tmp/urunc-vmm-api.sock` inside the monitor's mount namespace). The socket
is reachable from the host through `/proc/<pid>/root`.

## Usage

### With the urunc CLI

```bash
# Checkpoint a running container into a directory and stop it:
urunc checkpoint --image-path /tmp/ckpt mycontainer

# ...or keep it running after the snapshot:
urunc checkpoint --image-path /tmp/ckpt --leave-running mycontainer

# Restore a new container instance from the checkpoint:
urunc restore --image-path /tmp/ckpt --bundle /path/to/bundle mycontainer2
```

`urunc` also implements `pause` and `resume`, which suspend and resume the
guest's vCPUs through the VMM API:

```bash
urunc pause mycontainer
urunc resume mycontainer
```

### With containerd

The `checkpoint`, `restore`, `pause` and `resume` commands are CLI-compatible
with `runc`, so the containerd shim's inherited task service drives them
directly, e.g.:

```bash
ctr task pause mycontainer
ctr task resume mycontainer
ctr task checkpoint --image-path /tmp/ckpt mycontainer
```

CRIU-specific flags (`--tcp-established`, `--file-locks`, etc.) are accepted
for compatibility but ignored: the VM snapshot always captures the complete
guest state, including open connections' guest-side state and the guest page
cache.

## What is captured

* **Guest memory and device state**: everything running inside the guest,
  including the in-memory state of the application.
* **urunc metadata** (`urunc-checkpoint.json`): the VMM type and the network
  parameters the VM was started with, used for validation and re-wiring at
  restore time.

The **rootfs is not captured**: a restored container must be created from
the same rootfs content the checkpointed container used (for block-based
rootfs, the same devmapper snapshot content at the same in-guest paths).
When restoring through containerd, the checkpoint image includes the
container's rw-layer diff, which containerd re-applies on restore.

## Networking across restore

The guest's network identity (MAC address, IP address, ARP/neighbor state)
is frozen inside the snapshot. On restore, `urunc` creates a fresh tap
device in the new network namespace and re-attaches the frozen guest NIC to
it (via `network_overrides` on Firecracker, or by rewriting the snapshot's
`config.json` on Cloud Hypervisor). For the restored guest to communicate:

* the new network namespace must provide an **equivalent L3 environment**
  (same guest IP, mask and gateway); with the default TC-redirect setup this
  is the case when the container's veth carries the same IP;
* TCP connections that were open at checkpoint time are **not** guaranteed
  to survive; the peer side will usually have timed out.

If the restore namespace has a different IP than the checkpointed one,
`urunc` logs a warning and proceeds: the guest keeps using its original
address.

## Limitations

* Only Firecracker and Cloud Hypervisor; snapshots are **not** portable
  across VMM types, and Firecracker additionally requires the same
  Firecracker version at restore.
* Restore requires the same CPU vendor/features on the target host.
* vAccel-enabled containers cannot be checkpointed/restored (the vsock
  device state references host paths that are not re-wired yet).
* Shared-fs (virtiofs/9p) rootfs containers are snapshottable only insofar
  as the shared directory content is unchanged at restore time; block and
  initrd rootfs types are recommended.
* Snapshot files are copied into the checkpoint directory (and staged back
  into the monitor rootfs on restore); for large guest memory sizes this
  adds a copy cost proportional to memory size.
