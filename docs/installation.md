This document outlines the steps required to set up an environment for running
both standard containers and `urunc`-based ones. It also includes the
installation of all `urunc` supported VM/Sandbox monitors.

Although the instructions assume a vanilla Ubuntu 22.04 system, `urunc` is
compatible with a variety of Linux distributions.

The installation guide is split in three parts:

**1. Installation of common container tools and containerd configuration**

This step involves the installation and configuration of several essential components
for a fully functional and reliable container environment. Specifically:

- [runc](https://github.com/opencontainers/runc)
- [containerd](https://github.com/containerd/containerd/)
- [CNI plugins](https://github.com/containernetworking/plugins)
- [nerdctl](https://github.com/containerd/nerdctl)
- [devmapper](https://docs.docker.com/storage/storagedriver/device-mapper-driver/) and/or [blockfile](https://github.com/containerd/containerd/blob/main/docs/snapshotters/blockfile.md)

**2. Installation of all supported monitors and additional tools**

This step installs all currently supported monitors of `urunc`, along
with virtiofsd. Specifically:

- [solo5-{hvt|spt}](https://github.com/Solo5/solo5)
- [qemu](https://www.qemu.org/)
- [firecracker](https://github.com/firecracker-microvm/firecracker)
- [virtiofsd](https://virtio-fs.gitlab.io/)

**3. Installation and configuration of `urunc`**

In the last step the installation of `urunc` will take place. This step also
provides information on how to build `urunc` from source using [Go [[
versions.go ]]](https://go.dev/doc/install)

Let's go.

> Note: Some of these steps may override existing tools or services. Please make
> sure to keep backups of any critical configurations.

## Step 1: Install container components and tools and containerd configuration

This section covers the installation of necessary container components and the
configuration of `containerd`.  If a functioning container setup with the
required tools is already present, this step can be skipped.

### Install runc or any other generic low level container runtime

In Kubernetes environments, `urunc` delegates the management of normal
containers (such as pause and sidecar containers) to a typical low-level
container runtime like `runc`,`crun`, `youki` etc.
For the runc installation either [follow
the instructions in `runc`'s
repository](https://github.com/opencontainers/runc/tree/main#building) or
download the latest release with the following commands:

```bash
RUNC_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/opencontainers/runc/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/opencontainers/runc/releases/download/v$RUNC_VERSION/runc.$(dpkg --print-architecture)
sudo install -m 755 runc.$(dpkg --print-architecture) /usr/local/sbin/runc
rm -f ./runc.$(dpkg --print-architecture)
```

### Install containerd

For the time being, `urunc` has been properly tested with
[containerd](https://github.com/containerd/containerd) as the high-level
container runtime. For installation methods or other information, please check
containerd's [Getting
Started](https://github.com/containerd/containerd/blob/main/docs/getting-started.md)
guide. The following commands download and install the latest release.

```bash
CONTAINERD_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/containerd/containerd/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/containerd/containerd/releases/download/v$CONTAINERD_VERSION/containerd-$CONTAINERD_VERSION-linux-$(dpkg --print-architecture).tar.gz
sudo tar Cxzvf /usr/local containerd-$CONTAINERD_VERSION-linux-$(dpkg --print-architecture).tar.gz
rm -f containerd-$CONTAINERD_VERSION-linux-$(dpkg --print-architecture).tar.gz
```

#### Install containerd service

To configure [containerd](https://github.com/containerd/containerd) to start
automatically on system boot, set up the corresponding [systemd](https://systemd.io/)
service:

```bash
CONTAINERD_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/containerd/containerd/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://raw.githubusercontent.com/containerd/containerd/v$CONTAINERD_VERSION/containerd.service
sudo rm -f /lib/systemd/system/containerd.service
sudo mv containerd.service /lib/systemd/system/containerd.service
sudo systemctl daemon-reload
sudo systemctl enable --now containerd
```

#### Install CNI plugins

For container networking the CNI plugins are necessary. The following commands
download and install in `/opt/cni/bin` the latest release.

```bash
CNI_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/containernetworking/plugins/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/containernetworking/plugins/releases/download/v$CNI_VERSION/cni-plugins-linux-$(dpkg --print-architecture)-v$CNI_VERSION.tgz
sudo mkdir -p /opt/cni/bin
sudo tar Cxzvf /opt/cni/bin cni-plugins-linux-$(dpkg --print-architecture)-v$CNI_VERSION.tgz
rm -f cni-plugins-linux-$(dpkg --print-architecture)-v$CNI_VERSION.tgz
```

#### Configure containerd

In case `containerd`'s configuration is missing, the default one can be created
with the following commands:

```bash
sudo mkdir -p /etc/containerd/
sudo mv /etc/containerd/config.toml /etc/containerd/config.toml.bak # There might be no existing configuration.
sudo containerd config default | sudo tee /etc/containerd/config.toml
sudo systemctl restart containerd
```

> To easily migrate containerd's configuration from older to newer versions
> execute `sudo containerd config migrate > /etc/containerd/config.toml`

### Block-based snapshotters

`urunc` can leverage block-based snapshots to treat a container's snapshot as a
block device for a guest. Currently, `urunc` has been tested and verified with
[devmapper](https://github.com/containerd/containerd/blob/main/docs/snapshotters/devmapper.md)
and
[blockfile](https://github.com/containerd/containerd/blob/main/docs/snapshotters/blockfile.md).
Devmapper uses a thinpool for flexible management, while blockfile uses on a
pre-allocated scratch file, though it lacks ext2 support and thus it is not
compatible with Rumprun unikernels.

#### Setting and configuring devmapper

To configure the devmapper thinpool, `urunc` repository contains two helper scripts
under the [script
directory](https://github.com/urunc-dev/urunc/tree/main/script/dm_create.sh).
The first
[dm\_create.sh](https://github.com/urunc-dev/urunc/tree/main/script/dm_create.sh)
creates a thinpool, while the second
[dm\_reload.sh](https://github.com/urunc-dev/urunc/tree/main/script/dm_reload.sh)
reloads the same thinpool that has been created from
[dm\_create.sh](https://github.com/urunc-dev/urunc/tree/main/script/dm_create.sh).

> Note: The scripts use the `bc` tool, which needs to be installed.

To install the scripts:

```bash
git clone https://github.com/urunc-dev/urunc.git
sudo mkdir -p /usr/local/bin/scripts
sudo mkdir -p /usr/local/lib/systemd/system/
sudo cp urunc/script/dm_create.sh /usr/local/bin/scripts/dm_create.sh
sudo cp urunc/script/dm_reload.sh /usr/local/bin/scripts/dm_reload.sh
sudo chmod 755 /usr/local/bin/scripts/dm_create.sh
sudo chmod 755 /usr/local/bin/scripts/dm_reload.sh
```

To create the thinpool:

```bash
sudo /usr/local/bin/scripts/dm_create.sh
```

However, when the system reboots, the thinpool needs to get reloaded:

```bash
sudo /usr/local/bin/scripts/dm_reload.sh
```

Alternatively, a [systemd](https://systemd.io/) service can automatically reload
the existing thinpool when a system reboots. The `urunc` repository contains
[such a
service](https://github.com/urunc-dev/urunc/tree/main/script/dm_reload.service)

```bash
sudo cp urunc/script/dm_reload.service /usr/local/lib/systemd/system/dm_reload.service
sudo chmod 644 /usr/local/lib/systemd/system/dm_reload.service
sudo chown root:root /usr/local/lib/systemd/system/dm_reload.service
sudo systemctl daemon-reload
sudo systemctl enable dm_reload.service
```

At last, update the containerd configuration for devmapper:

- In containerd v2.x find and change or append the following lines:

```toml
[plugins.'io.containerd.snapshotter.v1.devmapper']
  pool_name = "containerd-pool"
  root_path = "/var/lib/containerd/io.containerd.snapshotter.v1.devmapper"
  base_image_size = "10GB"
  discard_blocks = true
  fs_type = "ext2"
```

- In containerd v1.x find and change or append the following lines:

```bash
[plugins."io.containerd.snapshotter.v1.devmapper"]
  pool_name = "containerd-pool"
  root_path = "/var/lib/containerd/io.containerd.snapshotter.v1.devmapper"
  base_image_size = "10GB"
  discard_blocks = true
  fs_type = "ext2"
```

After updating the configuration, containerd needs to restart:

```bash
sudo systemctl restart containerd
```

Verify that the devmapper snapshotter is properly configured with:

```bash
$ sudo ctr plugin ls | grep devmapper
io.containerd.snapshotter.v1              devmapper                linux/amd64    ok
```

#### Setting and configuring blockfile

The first step of setting up `blockfile` is the creation of the
scratch file, which will be used from the snapshotter:

```bash
sudo mkdir -p /opt/containerd/blockfile
sudo dd if=/dev/zero of=/opt/containerd/blockfile/scratch bs=1M count=500
sudo mkfs.ext4 /opt/containerd/blockfile/scratch
sudo chown -R root:root /opt/containerd/blockfile
```

After the creation of the scratch file, update the containerd's configuration
for the blockfile snapshotter:

- In containerd v2.x find and change or append the following lines:

```toml
[plugins.'io.containerd.snapshotter.v1.blockfile']
  fs_type = "ext4"
  mount_options = []
  recreate_scratch = true
  root_path = "/var/lib/containerd/io.containerd.snapshotter.v1.blockfile"
  scratch_file = "/opt/containerd/blockfile/scratch"
  supported_platforms = ["linux/amd64"]
```

- In containerd v1.x find and change or append the following lines:

```toml
[plugins."io.containerd.snapshotter.v1.blockfile"]
  fs_type = "ext4"
  mount_options = []
  recreate_scratch = true
  root_path = "/var/lib/containerd/io.containerd.snapshotter.v1.blockfile"
  scratch_file = "/opt/containerd/blockfile/scratch"
  supported_platforms = ["linux/amd64"]
```

> Blockfile configuration options:
>
> - `root_path`: Directory for storing block files (must be writable by containerd).
> - `fs_type`: Filesystem type for block files (supported: ext4)
> - `scratch_file`: The path to the empty file that will be used as the base for the block files.
> - `recreate_scratch`: If set to true, the snapshotter will recreate the scratch file if it is missing.

After updating the configuration, containerd needs to restart:

```bash
sudo systemctl restart containerd
```

Verify that the blockfile snapshotter is properly configured with:

```bash
$ sudo ctr plugin ls | grep blockfile
   io.containerd.snapshotter.v1           blockfile               linux/amd64    ok
```

### Install nerdctl

To easily interact with containerd,
[nerdctl](https://github.com/containerd/nerdctl) offers a Docker-compatible CLI
experience. The following commands download and install the latest release.

```bash
NERDCTL_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/containerd/nerdctl/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/containerd/nerdctl/releases/download/v$NERDCTL_VERSION/nerdctl-$NERDCTL_VERSION-linux-$(dpkg --print-architecture).tar.gz
sudo tar Cxzvf /usr/local/bin nerdctl-$NERDCTL_VERSION-linux-$(dpkg --print-architecture).tar.gz
rm -f nerdctl-$NERDCTL_VERSION-linux-$(dpkg --print-architecture).tar.gz
```

## Step 2: Install all supported monitors

This section includes the installation of all supported monitors and
`virtiofsd`. For ease of use, the
[monitors-build repository](https://github.com/urunc-dev/monitors-build)
contains releases with various versions of these monitors and tools.
Alternatively, each monitor can be downloaded and installed following the
respective installation guide.

### Option 1: Using the monitors-build repository

The [monitor-builds repository](https://github.com/urunc-dev/monitors-build)
provides a reference setup for building and distributing static binaries of
monitors and tools for `urunc`. In the [releases
page](https://github.com/urunc-dev/monitors-build/releases) there are archives
with all monitor artifacts for specific versions. However, users can request the
creation of a new release with different versions. More information can be found
in the [respective section of the repository's README
file](https://github.com/urunc-dev/monitors-build?tab=readme-ov-file#how-to-use).

As an example, the following commands use the
[`FC-v1.7.0_S5-v0.9.3_VFS_-v1.13.0_QM-v10.1.1-9a44e`
release](https://github.com/urunc-dev/monitors-build/releases/tag/FC-v1.7.0_S5-v0.9.3_VFS_-v1.13.0_QM-v10.1.1-9a44e)
which contains the following monitors and tools in the specified versions:

- Firecracker v1.7.0
- Solo5 v0.9.3
- Virtiofsd v1.13.0
- Qemu v10.1.1

To download and install the monitors in `/tmp`:
```
wget https://github.com/urunc-dev/monitors-build/releases/download/FC-v1.7.0_S5-v0.9.3_VFS_-v1.13.0_QM-v10.1.1-9a44e/release-amd64-FC-v1.7.0_S5-v0.9.3_VFS_-v1.13.0_QM-v10.1.1-9a44e.tar.gz
sudo tar Cxzvf /opt release-amd64-FC-v1.7.0_S5-v0.9.3_VFS_-v1.13.0_QM-v10.1.1-9a44e.tar.gz
rm -f release-amd64-FC-v1.7.0_S5-v0.9.3_VFS_-v1.13.0_QM-v10.1.1-9a44e.tar.gz
```

After downloading all the binaries, we need to instruct `urunc` about the
location of the binaries. Therefore, in [`urunc`'s
configuration](../configuration). there are three fields that need to
get updated:

1. in each monitor the `path` field,
2. in Qemu, the `data_path` field,
3. in Virtiofsd, the `path` field needs to get updated.

Therefore, change or append the following lines in `urunc`'s configuration:

```
[monitors.qemu]
path = "/opt/urunc/bin/qemu-system-x86_64"
data_path = "/opt/urunc/share/qemu"

[monitors.firecracker]
path = "/opt/urunc/bin/firecracker"

[monitors.hvt]
path = "/opt/urunc/bin/solo5-hvt"

[monitors.spt]
path = "/opt/urunc/bin/solo5-spt"

[extra_binaries.virtiofsd]
path = "/opt/urunc/bin/virtiofsd"
```

### Option 2: Fetching or building from source

Alternatively, each monitor can be simply downloaded or built from source.

#### Solo5

In the case of Solo5, there is only the option of building from source cloning
the [ respective repository](https://github.com/Solo5/solo5) and using the commands below.

```bash
git clone -b v[[ versions.solo5 ]] https://github.com/Solo5/solo5.git
cd solo5
./configure.sh  && make -j$(nproc)
sudo cp tenders/hvt/solo5-hvt /usr/local/bin
sudo cp tenders/spt/solo5-spt /usr/local/bin
```

> NOTE: For the `solo5-spt` monitor `libseccomp-dev` is necessary.

### Qemu

[Qemu](https://www.qemu.org/) is a popular VMM and emulator which is available
as a package from the vast majority of Linux distributions. In apt-based
distributions:

```bash
sudo apt install qemu-system
```

### Firecracker

[firecracker](https://github.com/firecracker-microvm/firecracker) provides
releases with statically-built binaries. Due to some
[issues](https://github.com/unikraft/unikraft/issues/1410) of Unikraft with
newer versions of Firecracker, the following commands install version 1.7.0:

```bash
ARCH="$(uname -m)"
VERSION="v[[ versions.firecracker ]]"
release_url="https://github.com/firecracker-microvm/firecracker/releases"
curl -L ${release_url}/download/${VERSION}/firecracker-${VERSION}-${ARCH}.tgz | tar -xz
# Rename the binary to "firecracker"
sudo mv release-${VERSION}-${ARCH}/firecracker-${VERSION}-${ARCH} /usr/local/bin/firecracker
rm -fr release-${VERSION}-${ARCH}
```

### Virtiofsd

As an alternative to 9pfs, `urunc` can configure Qemu to use
[virtiofs](https://virtio-fs.gitlab.io/). To do that, `virtiofsd` is necessary.
By default `urunc` searches for the `virtiofsd` binary under `/usr/libexec`.
However, the path for virtiofsd can be set in the respective section of
[`urunc`'s configuration](../configuration#Extra-binaries-Configuration)
The following commands download and install the latest
release from the gitlab [repository of
virtiofsd](https://gitlab.com/virtio-fs/virtiofsd):

```
wget https://gitlab.com/-/project/21523468/uploads/0298165d4cd2c73ca444a8c0f6a9ecc7/virtiofsd-v1.13.2.zip
unzip virtiofsd-v1.13.2.zip
sudo mv target/x86_64-unknown-linux-musl/release/virtiofsd /usr/libexec/
rm -rf target virtiofsd-v1.13.2.zip
```

## Step 3: Install urunc and configure containerd

### Installing urunc

To install `urunc`, there are three options:

1. building from source,
2. grabbing the binaries from the latest release, or
3. grabbing the binaries from the lastest commit in main.

#### Option 1: Building from source

In order to build `urunc` from source, any earlier version of Go 1.20.6 should
be sufficient, Let's download Go [[versions.go ]]

```bash
GO_VERSION=[[ versions.go ]]
wget -q https://go.dev/dl/go${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz
sudo mkdir /usr/local/go${GO_VERSION}
sudo tar -C /usr/local/go${GO_VERSION} -xzf go${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz
sudo tee -a /etc/profile > /dev/null << EOT
export PATH=\$PATH:/usr/local/go$GO_VERSION/go/bin
EOT
rm -f go${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz
```

> Note: A re-login to the shell might be necessary for the `PATH` update.

With Go installed, building and installing `urunc` is as easy as executing the
following commands:

```bash
git clone https://github.com/urunc-dev/urunc.git
cd urunc
make && sudo make install
cd ..
```

#### Option 2: Install latest release

Alternatively, to get the latest
[release](https://github.com/urunc-dev/urunc/releases):

```bash
URUNC_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/urunc-dev/urunc/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
URUNC_BINARY_FILENAME="urunc_static_v${URUNC_VERSION}_$(dpkg --print-architecture)"
wget -q https://github.com/urunc-dev/urunc/releases/download/v$URUNC_VERSION/$URUNC_BINARY_FILENAME
chmod +x $URUNC_BINARY_FILENAME
sudo mv $URUNC_BINARY_FILENAME /usr/local/bin/urunc
```

And for `containerd-shim-urunc-v2`:

```bash
CONTAINERD_BINARY_FILENAME="containerd-shim-urunc-v2_static_v${URUNC_VERSION}_$(dpkg --print-architecture)"
wget -q https://github.com/urunc-dev/urunc/releases/download/v$URUNC_VERSION/$CONTAINERD_BINARY_FILENAME
chmod +x $CONTAINERD_BINARY_FILENAME
sudo mv $CONTAINERD_BINARY_FILENAME /usr/local/bin/containerd-shim-urunc-v2
```

#### Option 3: Install from latest artifacts (tip of the main branch)

Alternatively, to get a `urunc` binary based on the main branch:

```bash
URUNC_VERSION=main
URUNC_BINARY_FILENAME="urunc_static_$(dpkg --print-architecture)"
wget -q https://s3.nbfc.io/nbfc-assets/github/urunc/dist/$URUNC_VERSION/$(dpkg --print-architecture)/$URUNC_BINARY_FILENAME
chmod +x $URUNC_BINARY_FILENAME
sudo mv $URUNC_BINARY_FILENAME /usr/local/bin/urunc
```

And for `containerd-shim-urunc-v2`:

```bash
CONTAINERD_BINARY_FILENAME="containerd-shim-urunc-v2_static_$(dpkg --print-architecture)"
wget -q https://s3.nbfc.io/nbfc-assets/github/urunc/dist/$URUNC_VERSION/$(dpkg --print-architecture)/$CONTAINERD_BINARY_FILENAME
chmod +x $CONTAINERD_BINARY_FILENAME
sudo mv $CONTAINERD_BINARY_FILENAME /usr/local/bin/containerd-shim-urunc-v2
```

### Add urunc runtime to containerd

At last, `urunc` needs to be configured as a runtime in containerd. Make sure to
set the chosen snapshotter (`devmapper` or `blockfile`).

- In containerd v2.x find and change or append the following lines:

```toml
[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.urunc]
    runtime_type = "io.containerd.urunc.v2"
    container_annotations = ["com.urunc.unikernel.*"]
    pod_annotations = ["com.urunc.unikernel.*"]
    snapshotter = "devmapper"
```

- In containerd v1.x find and change or append the following lines:

```toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.urunc]
    runtime_type = "io.containerd.urunc.v2"
    container_annotations = ["com.urunc.unikernel.*"]
    pod_annotations = ["com.urunc.unikernel.*"]
    snapshotter = "devmapper"
```

After updating the configuration, containerd needs to restart:

```bash
sudo systemctl restart containerd
```

## Run example unikernels

Now, let's run some unikernels for every VM/Sandbox monitor, to make sure
everything was installed correctly.

#### Run a Redis Rumprun unikernel over Solo5-hvt

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/redis-hvt-rumprun-block:latest
```

#### Run a Redis rumprun unikernel over Solo5-spt with devmapper

```bash
sudo nerdctl run --rm -ti --snapshotter devmapper --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/redis-spt-rumprun-raw:latest
```

#### Run a Nginx Unikraft unikernel over Qemu

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/nginx-qemu-unikraft-initrd:latest
```

#### Run a Nginx Unikraft unikernel over Firecracker

```bash
sudo nerdctl run --rm -ti --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/nginx-firecracker-unikraft-initrd:latest
```
