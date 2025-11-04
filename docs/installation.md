This document guides you through the installation of `urunc` and all required
components for executing all supported unikernels and VM/Sandbox monitors.

We assume a vanilla ubuntu 22.04 environment, although `urunc` is able to run
on a number of distros.

We will be installing and setting up:

- git, wget, bc, make, build-essential
- [runc](https://github.com/opencontainers/runc)
- [containerd](https://github.com/containerd/containerd/)
- [CNI plugins](https://github.com/containernetworking/plugins)
- [nerdctl](https://github.com/containerd/nerdctl)
- [devmapper](https://docs.docker.com/storage/storagedriver/device-mapper-driver/) or [blockfile](https://github.com/containerd/containerd/blob/main/docs/snapshotters/blockfile.md)
- [virtiofs](https://virtio-fs.gitlab.io/)
- [Go [[ versions.go ]]](https://go.dev/doc/install)
- [urunc](https://github.com/urunc-dev/urunc)
- [solo5-{hvt|spt}](https://github.com/Solo5/solo5)
- [qemu](https://www.qemu.org/)
- [firecracker](https://github.com/firecracker-microvm/firecracker)

Let's go.

> Note: Be aware that some instructions might override existing tools and services.

`urunc` offers two snapshotter options for unikernel block device snapshots: **devmapper** and **blockfile**.
Devmapper uses a thinpool for flexible management, while blockfile relies on a pre-allocated scratch file,
though it lacks ext2 support and thus isn’t compatible with Rumprun unikernels.

## Install required dependencies

The following packages are required to complete the installation. Depending
on your specific needs, some of them may not be necessary in your use case.

```bash
sudo apt install git wget build-essential libseccomp-dev pkg-config
```

## Install container-related dependencies

### Install runc or any other generic container runtime

`urunc` requires a typical container runtime (e.g. runc, crun) to handle any
unsupported container images (for
example, in k8s pods the pause container is delegated to `runc` and urunc
handles only the unikernel container). In this guide we will use `runc`.
You can [build runc from
source](https://github.com/opencontainers/runc/tree/main#building) or download
the latest binary following the commands:

```bash
RUNC_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/opencontainers/runc/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/opencontainers/runc/releases/download/v$RUNC_VERSION/runc.$(dpkg --print-architecture)
sudo install -m 755 runc.$(dpkg --print-architecture) /usr/local/sbin/runc
rm -f ./runc.$(dpkg --print-architecture)
```

### Install containerd

We will use [containerd](https://github.com/containerd/containerd) as a
high-level runtime and its latest version. For alternative
installation methods or other information, please check containerd's [Getting
Started](https://github.com/containerd/containerd/blob/main/docs/getting-started.md)
guide.

```bash
CONTAINERD_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/containerd/containerd/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/containerd/containerd/releases/download/v$CONTAINERD_VERSION/containerd-$CONTAINERD_VERSION-linux-$(dpkg --print-architecture).tar.gz
sudo tar Cxzvf /usr/local containerd-$CONTAINERD_VERSION-linux-$(dpkg --print-architecture).tar.gz
rm -f containerd-$CONTAINERD_VERSION-linux-$(dpkg --print-architecture).tar.gz
```

#### Install containerd service

To start [containerd](https://github.com/containerd/containerd) with
[systemd](https://systemd.io/), we will need to setup the respective service.

```bash
CONTAINERD_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/containerd/containerd/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://raw.githubusercontent.com/containerd/containerd/v$CONTAINERD_VERSION/containerd.service
sudo rm -f /lib/systemd/system/containerd.service
sudo mv containerd.service /lib/systemd/system/containerd.service
sudo systemctl daemon-reload
sudo systemctl enable --now containerd
```

#### Configure containerd

We will generate the default containerd's configuration to build on top of it
later.

```bash
sudo mkdir -p /etc/containerd/
sudo mv /etc/containerd/config.toml /etc/containerd/config.toml.bak # There might be no existing configuration.
sudo containerd config default | sudo tee /etc/containerd/config.toml
sudo systemctl restart containerd
```

#### Install CNI plugins

To install the latest release of CNI plugins:

```bash
CNI_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/containernetworking/plugins/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/containernetworking/plugins/releases/download/v$CNI_VERSION/cni-plugins-linux-$(dpkg --print-architecture)-v$CNI_VERSION.tgz
sudo mkdir -p /opt/cni/bin
sudo tar Cxzvf /opt/cni/bin cni-plugins-linux-$(dpkg --print-architecture)-v$CNI_VERSION.tgz
rm -f cni-plugins-linux-$(dpkg --print-architecture)-v$CNI_VERSION.tgz
```

### Install nerdctl

To install the latest release of `nerdctl`:

```bash
NERDCTL_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/containerd/nerdctl/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/containerd/nerdctl/releases/download/v$NERDCTL_VERSION/nerdctl-$NERDCTL_VERSION-linux-$(dpkg --print-architecture).tar.gz
sudo tar Cxzvf /usr/local/bin nerdctl-$NERDCTL_VERSION-linux-$(dpkg --print-architecture).tar.gz
rm -f nerdctl-$NERDCTL_VERSION-linux-$(dpkg --print-architecture).tar.gz
```
### Option 1: Devmapper
#### Setup thinpool devmapper

In order to make use of directly passing the container's snapshot as block
device in the unikernel, we will need to setup the devmapper snapshotter. We can
do that by first creating a thinpool, using the respective [scripts in urunc's
repo](https://github.com/urunc-dev/urunc/tree/main/script).

```bash
git clone https://github.com/urunc-dev/urunc.git
sudo mkdir -p /usr/local/bin/scripts
sudo mkdir -p /usr/local/lib/systemd/system/
sudo cp urunc/script/dm_create.sh /usr/local/bin/scripts/dm_create.sh
sudo cp urunc/script/dm_reload.sh /usr/local/bin/scripts/dm_reload.sh
sudo chmod 755 /usr/local/bin/scripts/dm_create.sh
sudo chmod 755 /usr/local/bin/scripts/dm_reload.sh
```

The above scripts create and reload respectively a thinpool that will be used
for the devmapper snapshotter. Therefore, to create the thinpool, we can run:

```bash
sudo /usr/local/bin/scripts/dm_create.sh
```

However, when the system reboots, we will need to reload the thinpool with:

```bash
sudo /usr/local/bin/scripts/dm_reload.sh
```

#### Create a service for thinpool reloading

Alternatively, we can automatically reload the existing thinpool when a system reboots,by setting
up a new service in [systemd](https://systemd.io/).

```bash
sudo cp urunc/script/dm_reload.service /usr/local/lib/systemd/system/dm_reload.service
sudo chmod 644 /usr/local/lib/systemd/system/dm_reload.service
sudo chown root:root /usr/local/lib/systemd/system/dm_reload.service
sudo systemctl daemon-reload
sudo systemctl enable dm_reload.service
```

#### Configure containerd for devmapper

- In containerd v2.x:

```bash
sudo sed -i "/\[plugins\.'io\.containerd\.snapshotter\.v1\.devmapper'\]/,/^$/d" /etc/containerd/config.toml
sudo tee -a /etc/containerd/config.toml > /dev/null <<'EOT'

# Customizations for devmapper

[plugins.'io.containerd.snapshotter.v1.devmapper']
  pool_name = "containerd-pool"
  root_path = "/var/lib/containerd/io.containerd.snapshotter.v1.devmapper"
  base_image_size = "10GB"
  discard_blocks = true
  fs_type = "ext2"
EOT
sudo systemctl restart containerd
```

- In containerd v1.x:

```bash
sudo sed -i '/\[plugins\."io\.containerd\.snapshotter\.v1\.devmapper"\]/,/^$/d' /etc/containerd/config.toml
sudo tee -a /etc/containerd/config.toml > /dev/null <<'EOT'

# Customizations for devmapper

[plugins."io.containerd.snapshotter.v1.devmapper"]
  pool_name = "containerd-pool"
  root_path = "/var/lib/containerd/io.containerd.snapshotter.v1.devmapper"
  base_image_size = "10GB"
  discard_blocks = true
  fs_type = "ext2"
EOT
sudo systemctl restart containerd
```

Before proceeding, make sure that the new snapshotter is properly configured:

```bash
sudo ctr plugin ls | grep devmapper
io.containerd.snapshotter.v1              devmapper                linux/amd64    ok
```

### Option 2: Blockfile
#### Using blockfile snapshotter (alternative to devmapper)

 `Urunc` can also use the [blockfile snapshotter](https://github.com/containerd/containerd/blob/main/docs/snapshotters/blockfile.md) as an alternative to devmapper for providing block device snapshots to unikernels.

#### Enabling the Blockfile Snapshotter

To configure the blockfile snapshotter, follow the steps below:

-  Create the blockfile scratch file

   First, create a directory and an appropriately-sized scratch file. For example:

   ```bash
   sudo mkdir -p /opt/containerd/blockfile
   sudo dd if=/dev/zero of=/opt/containerd/blockfile/scratch bs=1M count=500
   sudo mkfs.ext4 /opt/containerd/blockfile/scratch
   sudo chown -R root:root /opt/containerd/blockfile
   ```

#### Configure containerd for blockfile

-  In containerd v2.x:

   ```bash
   [plugins.'io.containerd.snapshotter.v1.blockfile']
     fs_type = "ext4"
     mount_options = []
     recreate_scratch = true
     root_path = "/var/lib/containerd/io.containerd.snapshotter.v1.blockfile"
     scratch_file = "/opt/containerd/blockfile/scratch"
     supported_platforms = ["linux/amd64"]
   ```

-  In containerd 1.x:

   ```bash
   [plugins."io.containerd.snapshotter.v1.blockfile"]
     fs_type = "ext4"
     mount_options = []
     recreate_scratch = true
     root_path = "/var/lib/containerd/io.containerd.snapshotter.v1.blockfile"
     scratch_file = "/opt/containerd/blockfile/scratch"
     supported_platforms = ["linux/amd64"]
   ```

- Blockfile configuration options:
   - `root_path`: Directory for storing block files (must be writable by containerd).
   - `fs_type`: Filesystem type for block files (supported: ext4)
   - `scratch_file`: The path to the empty file that will be used as the base for the block files.
   - `recreate_scratch`: If set to true, the snapshotter will recreate the scratch file if it is missing.

- Migrating configuration

Older syntax can be automatically converted to the latest version using the following command:

```bash
sudo containerd config migrate > /etc/containerd/config.toml
```

-  Restart the containerd service

   Restart containerd:

   ```bash
   sudo systemctl restart containerd
   ```

-  Verify the blockfile snapshotter is available

   Confirm that the blockfile snapshotter is registered and ready:

   ```bash
   sudo ctr plugin ls | grep blockfile
   ```

   The output should include a line similar to:

   ```bash
   io.containerd.snapshotter.v1           blockfile               linux/amd64    ok
   ```

## Install virtiofs

As an alternative to 9pfs, `urunc` is able to spawn guests using virtiofs. For that
purpose, we need to install virtiofsd under `/usr/libexec` where `urunc` expects to find
the statically linked binary of `virtiofsd`. For easier installation, we have uploaded
the respective binary in `https://s3.nbfc.io/nbfc-assets/github/urunc/bin/virtiofsd`.
Therefore, the installation can easily take place with the following instructions:

```
wget https://s3.nbfc.io/nbfc-assets/github/urunc/bin/virtiofsd
chmod +x virtiofsd
sudo cp virtiofsd /usr/libexec/
```

## Install urunc

### Option 1: Build from source

#### Install Go

In order to build `urunc` from source, we need to install Go.
Any version earlier than Go 1.20.6 will be sufficient.

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

> Note: You might need to logout and log back in to the shell, in order to use
> Go.

#### Build and install urunc

After installing Go, we can clone and build `urunc`:

```bash
git clone https://github.com/urunc-dev/urunc.git
cd urunc
make && sudo make install
cd ..
```

### Option 2: Install latest release

We can also install `urunc` from its latest
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

### Option 3: Install from latest artifacts (tip of the main branch)

We can also install `urunc` from binary builds of the main branch:

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

We also need to add `urunc` as a runtime in containerd's configuration:

- In containerd 2.x:

```bash
sudo tee -a /etc/containerd/config.toml > /dev/null <<EOT
[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.urunc]
    runtime_type = "io.containerd.urunc.v2"
    container_annotations = ["com.urunc.unikernel.*"]
    pod_annotations = ["com.urunc.unikernel.*"]
    snapshotter = "devmapper"
EOT
sudo systemctl restart containerd
```

- In containerd 1.x:

```bash
sudo tee -a /etc/containerd/config.toml > /dev/null <<EOT
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.urunc]
    runtime_type = "io.containerd.urunc.v2"
    container_annotations = ["com.urunc.unikernel.*"]
    pod_annotations = ["com.urunc.unikernel.*"]
    snapshotter = "devmapper"
EOT
sudo systemctl restart containerd
```

## Install Qemu, Firecracker and Solo5

### Install Solo5

We can clone, build and install both `Solo5-hvt` and `Solo5-spt` from their [common repository](https://github.com/Solo5/solo5)

```bash
git clone -b v[[ versions.solo5 ]] https://github.com/Solo5/solo5.git
cd solo5
./configure.sh  && make -j$(nproc)
sudo cp tenders/hvt/solo5-hvt /usr/local/bin
sudo cp tenders/spt/solo5-spt /usr/local/bin
```

### Install Qemu

Qemu installation can easily take place using the package manager.

```bash
sudo apt install qemu-system
```

### Install Firecracker

To install firecracker, we will use the github release page of Firecracker.
We choose to install version 1.7.0, since Unikraft has some
[issues](https://github.com/unikraft/unikraft/issues/1410) with newer versions.

```bash
ARCH="$(uname -m)"
VERSION="v[[ versions.firecracker ]]"
release_url="https://github.com/firecracker-microvm/firecracker/releases"
curl -L ${release_url}/download/${VERSION}/firecracker-${VERSION}-${ARCH}.tgz | tar -xz
# Rename the binary to "firecracker"
sudo mv release-${VERSION}-${ARCH}/firecracker-${VERSION}-${ARCH} /usr/local/bin/firecracker
rm -fr release-${VERSION}-${ARCH}
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
