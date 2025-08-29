---
title: Setup a Dev environment
description: "Dev environment setup guide"
---

## Prerequisites

Most of the steps are covered in the [installation](../../installation) document.
Please refer to it for:

- installing a recent version of Go (e.g. [[ versions.go ]])
- installing `containerd` and `runc`
- setting up the devmapper snapshotter
- installing `nerdctl` and the `CNI` plugins
- installing the relevant hypervisors

## crictl installation

In addition to the above, we strongly suggest to install
[crictl](https://github.com/kubernetes-sigs/cri-tools/tree/master) which `urunc`
uses for its end-to-end tests. The following commands will install `crictl`

```bash
VERSION="v1.30.0" # check latest version in /releases page
wget https://github.com/kubernetes-sigs/cri-tools/releases/download/$VERSION/crictl-$VERSION-linux-amd64.tar.gz
sudo tar zxvf crictl-$VERSION-linux-amd64.tar.gz -C /usr/local/bin
rm -f crictl-$VERSION-linux-amd64.tar.gz
```

Since default endpoints for `crictl` are now deprecated, we need to set them up:

```bash
sudo tee -a /etc/crictl.yaml > /dev/null <<'EOT'
runtime-endpoint: unix:///run/containerd/containerd.sock
image-endpoint: unix:///run/containerd/containerd.sock
timeout: 20
EOT
```

## urunc installation

The next step is to clone and build `urunc`:

```bash
git clone https://github.com/urunc-dev/urunc.git
cd urunc
make && sudo make install
```

At last, please  validate that the dev environment has been set correctly
by running the:

- unit tests: `make unittest` and

- end-to-end tests: `sudo make e2etest`

> Note: When running `make` commands for `urunc` that will use go (i.e. build,
> unitest, e2etest) you might need to specify the path to the go binary
with `sudo GO=$(which go) make`.

## Debugging

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
