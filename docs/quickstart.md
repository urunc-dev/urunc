This document acts as a quickstart guide to showcase `urunc` features. Please
refer to the [installation guide](../installation) for more detailed installation
instructions, or the [design](../design#architecture) document for more
details regarding `urunc`'s architecture.

We can quickly set up `urunc` either with [docker](https://docs.docker.com/engine/install/ubuntu/) or [containerd](https://github.com/containerd/containerd) and [nerdctl](https://github.com/containerd/nerdctl/).
We assume a vanilla ubuntu 22.04 environment, although `urunc` is able to run
on a number of GNU/Linux distributions.

## Using Docker

The easiest and fastest way to try out `urunc` would be with `docker`
Before doing so, please make sure that the host system satisfies the
following dependencies:

- [Docker](https://docs.docker.com/engine/install/ubuntu/)
- [Qemu](https://www.qemu.org/)
- `urunc` and `containerd-shim-urunc-v2` binaries.

### Install Docker

At first we need [docker](https://docs.docker.com/engine/install/ubuntu/).

```bash
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
rm get-docker.sh
sudo groupadd docker # The group might already exist
sudo usermod -aG docker $USER
```

> Note: Please logout and log back in from the shell, in order to be able to use
> docker without sudo

### Install `urunc` from source

Then we need `urunc`:

```bash
sudo apt install -y git make
git clone https://github.com/urunc-dev/urunc.git
docker run --rm -ti -v $PWD/urunc:/urunc -w /urunc golang:1.25 bash -c "git config --global --add safe.directory /urunc && make"
sudo make -C urunc install
```

### A docker example

We will try out a Unikraft unikernel over [Qemu](https://www.qemu.org/).

#### Install Qemu

Let's make sure that [Qemu](https://www.qemu.org/download/) is installed:

```bash
sudo apt install -y qemu-system
```

#### Run the unikernel

Now we are ready to run Nginx as a Unikraft unikernel using [docker](https://docs.docker.com/engine/install/ubuntu/) and `urunc`:

```console
$ docker run --rm -d --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/nginx-qemu-unikraft-initrd:latest
67bec5ab9a748e35faf7c2079002177b9bdc806220e59b6b413836db1d6e4018
```

We can inspect the container and get its IP address:

```console
$ docker inspect 67bec5ab9a748e35faf7c2079002177b9bdc806220e59b6b413836db1d6e4018 | grep IPAddress
            "SecondaryIPAddresses": null,
            "IPAddress": "172.17.0.2",
                    "IPAddress": "172.17.0.2",
```

At last we can curl the Nginx server running inside Unikraft with:

```console
$ curl 172.17.0.2
<!DOCTYPE html>
<html>
<head>
  <title>Hello, world!</title>
</head>
<body>
  <h1>Hello, world!</h1>
  <p>Powered by <a href="http://unikraft.org">Unikraft</a>.</p>
</body>
</html>
```
## Using containerd and nerdctl

The second way to quickly start with `urunc` would be by setting up a high-level
container runtime (e.g. [containerd](https://github.com/containerd/containerd)) and using [nerdctl](https://github.com/containerd/nerdctl/).

### Install a high-level container runtime

First step is to install [containerd](https://github.com/containerd/containerd) and
setup basic functionality (the `CNI` plugins and a snapshotter).

If a tool is already installed, skip to the next step.

#### Install and configure containerd

We will install [containerd](https://github.com/containerd/containerd) from the
package manager:

```bash
sudo apt install containerd
```

In this way we will also install `runc`, but not the necessary CNI plugins.
However, before proceeding to CNI plugins, we will generate the default
configuration for [containerd](https://github.com/containerd/containerd).

```bash
sudo mkdir -p /etc/containerd/
sudo mv /etc/containerd/config.toml /etc/containerd/config.toml.bak # There might be no configuration
sudo containerd config default | sudo tee /etc/containerd/config.toml
sudo systemctl restart containerd
```

#### Install CNI plugins

```bash
CNI_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/containernetworking/plugins/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/containernetworking/plugins/releases/download/v$CNI_VERSION/cni-plugins-linux-$(dpkg --print-architecture)-v$CNI_VERSION.tgz
sudo mkdir -p /opt/cni/bin
sudo tar Cxzvf /opt/cni/bin cni-plugins-linux-$(dpkg --print-architecture)-v$CNI_VERSION.tgz
rm -f cni-plugins-linux-$(dpkg --print-architecture)-v$CNI_VERSION.tgz
```

#### Setup thinpool devmapper

In order to make use of directly passing the container's snapshot as block
device in the unikernel, we will need to setup the devmapper snapshotter. We can
do that by first creating a thinpool, using the respective
[scripts in `urunc`'s repo](https://github.com/urunc-dev/urunc/tree/main/script)

```bash
wget -q https://raw.githubusercontent.com/urunc-dev/urunc/refs/heads/main/script/dm_create.sh
wget -q https://raw.githubusercontent.com/urunc-dev/urunc/refs/heads/main/script/dm_reload.sh
sudo mkdir -p /usr/local/bin/scripts
sudo mv dm_create.sh /usr/local/bin/scripts/dm_create.sh
sudo mv dm_reload.sh /usr/local/bin/scripts/dm_reload.sh
sudo chmod 755 /usr/local/bin/scripts/dm_create.sh
sudo chmod 755 /usr/local/bin/scripts/dm_reload.sh
sudo /usr/local/bin/scripts/dm_create.sh
```

> Note: The above instructions will create the thinpool, but in case of reboot,
> you will need to reload it running the `dm_reload.sh` script. Otherwise
> check the [installation guide for creating a service](../installation#create-a-service-for-thinpool-reloading). 

At last, we need to modify
[containerd](https://github.com/containerd/containerd/tree/main) configuration
for the new devmapper snapshotter:

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

Let's verify that the new snapshotter is properly configured:

```console
$ sudo ctr plugin ls | grep devmapper
io.containerd.snapshotter.v1           devmapper                linux/amd64    ok
```

### Install nerdctl

After installing [containerd](https://github.com/containerd/containerd) a nifty tool like [nerdctl](https://github.com/containerd/nerdctl/) is useful to get a realistic experience.

```bash
NERDCTL_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/containerd/nerdctl/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/containerd/nerdctl/releases/download/v$NERDCTL_VERSION/nerdctl-$NERDCTL_VERSION-linux-$(dpkg --print-architecture).tar.gz
sudo tar Cxzvf /usr/local/bin nerdctl-$NERDCTL_VERSION-linux-$(dpkg --print-architecture).tar.gz
rm -f nerdctl-$NERDCTL_VERSION-linux-$(dpkg --print-architecture).tar.gz
```

### Install `urunc` from its latest release

At last, but not least, we will install `urunc` from its latest release. At first, we
will install the `urunc` binary:

```bash
URUNC_VERSION=$(curl -L -s -o /dev/null -w '%{url_effective}' "https://github.com/urunc-dev/urunc/releases/latest" | grep -oP "v\d+\.\d+\.\d+" | sed 's/v//')
wget -q https://github.com/urunc-dev/urunc/releases/download/v$URUNC_VERSION/urunc_$(dpkg --print-architecture)
chmod +x urunc_$(dpkg --print-architecture)
sudo mv urunc_$(dpkg --print-architecture) /usr/local/bin/urunc
```

Secondly, we will install the `containerd-shim-urunc-v2` binary:

```bash
wget -q https://github.com/urunc-dev/urunc/releases/download/v$URUNC_VERSION/containerd-shim-urunc-v2_$(dpkg --print-architecture)
chmod +x containerd-shim-urunc-v2_$(dpkg --print-architecture)
sudo mv containerd-shim-urunc-v2_$(dpkg --print-architecture) /usr/local/bin/containerd-shim-urunc-v2
```

### A nerdctl-containerd example

We will try out a Rumprun unikernel running over Solo5-hvt with [nerdctl](https://github.com/containerd/nerdctl).

#### Install solo5

Lets install `solo5-hvt`:

```bash
sudo apt install make gcc pkg-config libseccomp-dev
git clone -b v[[ versions.solo5 ]] https://github.com/Solo5/solo5.git
cd solo5
./configure.sh  && make -j$(nproc)
sudo cp tenders/hvt/solo5-hvt /usr/local/bin
```

#### Run the Unikernel!

Now, let's run a Redis unikernel on top of Rumprun and solo5-hvt:

```bash
sudo nerdctl run -d --snapshotter devmapper --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/redis-hvt-rumprun-raw:latest
```

We can inspect the running container to check it's IP address:

```console
$ sudo nerdctl ps 
CONTAINER ID    IMAGE                                                      COMMAND        CREATED           STATUS    PORTS    NAMES
8a415b278a9e    harbor.nbfc.io/nubificus/urunc/redis-hvt-rumprun-raw:latest    "redis-server /data/…"    18 seconds ago    Up                 redis-hvt-rumprun-8a415
$ sudo nerdctl inspect 8a415b278a9e | grep IPAddress
            "IPAddress": "10.4.0.2",
                    "IPAddress": "10.4.0.2",
                    "IPAddress": "172.16.1.2",
```

and we can interact with the redis unikernel:

```console
$ telnet 10.4.0.2 6379
Trying 10.4.0.2...
Connected to 10.4.0.2.
Escape character is '^]'.
ping
+PONG
quit
+OK
Connection closed by foreign host.
```
