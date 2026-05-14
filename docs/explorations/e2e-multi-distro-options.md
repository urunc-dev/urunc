# Exploration: Multi-Distribution E2E Testing for urunc

## Objective
As part of Issue #208, we explored options to expand the E2E test suite to distributions beyond Ubuntu (the default GitHub Actions runner).

## Options Explored

### 1. GitHub Actions Job Containers
**Description**: Use the `container:` property in GitHub Actions jobs to run steps inside a specific Docker image (e.g., Fedora, Rocky Linux, OpenSUSE).
- **Pros**:
    - Natively supported by GitHub Actions.
    - Easy to matrix over different images.
    - Fast setup compared to full VMs.
- **Cons**:
    - No native `systemd` support in standard containers (requires workarounds for `containerd`).
    - Requires passing `/dev/kvm` and other host devices to the container.
    - Path differences and package manager variations must be handled in scripts.

### 2. Vagrant with KVM/QEMU
**Description**: Use Vagrant to provision full virtual machines on the GHA runner.
- **Pros**:
    - Full OS isolation with `systemd` and kernel modules.
    - Closest to real-world production environments.
- **Cons**:
    - Requires nested virtualization support on the runner.
    - Significant overhead and slower execution.
    - Complex setup in GHA (managing providers like libvirt/virtualbox).

### 3. Docker-in-Docker (Kind-like approach)
**Description**: Run tests inside a container that is started via `docker run` from the host, similar to how `kind_test.yml` works.
- **Pros**:
    - Allows fine-grained control over container options (privileged, volume mounts).
- **Cons**:
    - Adds an extra layer of abstraction.
    - Scripting becomes more complex (`docker exec` everywhere).

## Proposed Implementation (PoC)

We chose to implement a **hybrid approach using Job Containers** for simplicity and integration with existing GHA workflows, while adding distro-agnostic logic to the setup scripts.

### Implementation Details
1.  **Workflow Matrix**: Updated `.github/workflows/vm_test.yml` to include a `distro` matrix.
2.  **Container Configuration**: Jobs for non-Ubuntu distros use specified container images.
3.  **Distro-Agnostic Setup**:
    - Replaced hardcoded `apt-get` with a logic that detects the package manager (`apt`, `dnf`, `zypper`).
    - Added conditional logic for `systemd` vs. direct process execution for `containerd`.
    - Ensured dependencies like `libseccomp` and `qemu` are correctly mapped to distro-specific package names.

## Conclusion
The Job Container approach provides a good balance between test coverage and complexity. While it requires bypassing `systemd` in some cases, it effectively validates `urunc`'s compatibility with different system libraries and package versions across various Linux distributions.
