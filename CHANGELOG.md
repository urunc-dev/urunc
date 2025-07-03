# v0.5.0

## What's Changed

### New features
* Add support for all namespaces, except user namespaces
* Add support for MirageOS
* Add support for `urunc_deploy` and allow the easy installation and configuration of `urunc`, along with monitors, in existing Kubernetes clusters.
* Add support for non-root monitor execution

### Internals
* Update Go to version 1.24.1
* Update the unikernel interface and allow the use of unikernel-specific cli options when we spawn the Monitor:
  * `MonitorBlockCli()`: For block specific cli options
  * `MonitorNetCli()`: For net specific cli options
  * `MonitorCli()`: For other monitor cli options
* Spawn the monitor from container's rootfs
* Fix handling of devmapper and container's rootfs path.
* Fix readiness probe environment variable for Knative


### Building and CI/CD
* Handle warnings during container operations in end-to-end testing
* Update runners
* Transition to Incus for our end-to-end testing
* Add workflow to cleanup stale issues/PRs

### Misc
* Update yaml in kubernetes tutorial
* Add maintainers and Code of Conduct
* Add EKS tutorial in our docs
* Add Knative tutorial in our docs
* Update documentation regarding unikernel packaging, adding various examples and cases


**Full Changelog**: https://github.com/nubificus/urunc/compare/v0.4.0...v0.5.0

# Previous Releases:

## v0.4.0

## What's Changed

#### New features

* Introduce support for seccomp in VMMs
* Support of block images inside `urunc`'s container image
* Support of configurable memory using memory limit from container's spec 
* Support for docker

#### Internals

- network cleanup: delete TC rules and TAP device upon killing the unikernel
- Enhance unikernel interface with functions to check supporting features: 
  - `Init()`  initializes the unikernel struct based on the unikernel arguments
  - `SupportsBlock()` returns a bool value, based on the block support of the respective unikernel.
  - `SupportsFS()` takes as an argument a filesystem type and checks if the unikernel supports that type.
- Partial unit tests for pkg/unikontainers
- Refactor devmapper snapshot handling
- Define new environment variable `USE_DEVMAPPER_AS_BLOCK`to use devmapper's snapshot as a block image for the unikernel
- Handle newer versions of Unikraft unikernels
- Enable NAT and IP forwarding in static networking

#### Annotations
* `com.urunc.unikernel.block`: Define the path to the block image for the unikernel inside the container image
* `com.urunc.unikernel.blkMntPoint`: Define the mountpoint of the block image for the unikernel
* `com.urunc.unikernel.unikernelVersion`: Specify the version of unikernel

#### Building and CI/CD
* Add action for unit testing
* Refactor Makefile and enhance its targets
* Restructure CI jobs and transition to ARC runners

#### Misc
* Bug fixes
* Refactor handling of normal containers and replaces constants in paths and annotations
* Unikraft FC boot on arm64
* Huge refactor and update of `urunc`'s documentation. The documentation is available at https://nubificus.github.io/urunc/

**Full Changelog**: [https://github.com/nubificus/urunc/compare/v0.3.0...v0.4.0](https://github.com/nubificus/urunc/compare/v0.3.0...v0.4.0)

## v0.3.0

### What's Changed
* Fix race condition of accessing the init socket on large number of containers by @cmainas in https://github.com/nubificus/urunc/pull/15
* Handle unikernels requiring initrd by @gntouts in https://github.com/nubificus/urunc/pull/16
* Execute hooks concurrently to improve performance by @gntouts in https://github.com/nubificus/urunc/pull/17
* Add support for booting Unikraft unikernels over Qemu by @cmainas in https://github.com/nubificus/urunc/pull/18
* Add timestamps to measure performance by @gntouts in https://github.com/nubificus/urunc/pull/19
* Add end-to-end tests for Qemu-Unikraft by @gntouts in https://github.com/nubificus/urunc/pull/20
* Introduce support to boot up Firecracker with initrd by @cmainas in https://github.com/nubificus/urunc/pull/21
* Refactor end-to-end tests, Add firecracker-unikraft tests by @gntouts in https://github.com/nubificus/urunc/pull/22
* Wrap timestamp collection in logging function by @gntouts in https://github.com/nubificus/urunc/pull/24
* Add installation instructions for hypervisors by @gntouts in https://github.com/nubificus/urunc/pull/25
* ci: Add action to build & append git trailer by @gntouts in https://github.com/nubificus/urunc/pull/27
* ci: Update shutdown flag by @ananos in https://github.com/nubificus/urunc/pull/31
* hypervisors: Add machine option by @gntouts in https://github.com/nubificus/urunc/pull/33
* internal: move constants to separate pkg by @gntouts in https://github.com/nubificus/urunc/pull/34
* ci: Remove generated ssh key after artifact creation by @ananos in https://github.com/nubificus/urunc/pull/35
* Add a CONTRIBUTING document by @cmainas in https://github.com/nubificus/urunc/pull/29
* Network: Add static network mode by @gntouts in https://github.com/nubificus/urunc/pull/30

### New Contributors
* @cmainas made their first contribution in https://github.com/nubificus/urunc/pull/15
* @ananos made their first contribution in https://github.com/nubificus/urunc/pull/31

**Full Changelog**: https://github.com/nubificus/urunc/compare/v0.2.0...v0.3.0

## v0.2.0

### Changelog

- ([1ae5d5b](https://github.com/nubificus/urunc/commit/1ae5d5ba514a061bf14dbf01035a986b5cfb26e4)) Update installation instructions, add linting instructions (@gntouts)

- ([89fa71c](https://github.com/nubificus/urunc/commit/89fa71cc35f0bb3ce019e4c7b861dd43f49ead6b)) Add tests, update workflow triggers (@gntouts)
    - Add end to end tests for hvt hypervisor and rumprun unikernels using ctr, nerdct, crictl.

- ([9271e4f](https://github.com/nubificus/urunc/commit/9271e4f2dd667c4c23b716ec10010ec6d7759671)) Refactor urunc to enhance code organization and maintainability (@gntouts)

    - Move urunc cmd tool code under cmd directory.
    - Introduce 'unikontainers' package to separate urunc cmd tool from the underlying logic responsible for handling unikernel containers.
    - Separate hypervisor and unikernel functionality into distinct packages.
    - Update solo5-hvt to v0.6.9
    - Rewrite IPC mechanism to allow for retrying failed communication attempts.
    - Use a runc-compatible logging configuration.

- ([3b577eb](https://github.com/nubificus/urunc/commit/3b577eb7fef8d83c3dbea5cbea5ca9d7e58d03fc)) Fix typo on Installation.md (#2) (@johnp41)

## v0.1.0

Initial v0.1.0 release of urunc.


# Release history
See
[CHANGELOG.md](https://github.com/urunc-dev/urunc/blob/main/CHANGELOG.md)
for more information on what changed in this and previous releases.
