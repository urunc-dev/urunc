# How to use urunc with k8s

This guide assumes you have a working Kubernetes cluster.

To use `urunc` in a k8s cluster there are 2 options:

- [Manual installation](#manual-installation)
- [Using urunc-deploy](#urunc-deploy)

## Manual Installation

### Install urunc

Before we start, we need to have working Kubernetes cluster with [urunc installed](../installation.md) on one or more nodes.

### Add urunc as a RuntimeClass

First, we need to add `urunc` as a runtime class for the k8s cluster:

```bash
cat << EOF | tee urunc-runtimeClass.yaml
kind: RuntimeClass
apiVersion: node.k8s.io/v1
metadata:
    name: urunc
handler: urunc
EOF

kubectl apply -f urunc-runtimeClass.yaml
```

To verify the runtimeClass was added:

```bash
kubectl get runtimeClass
```

### Create a test deployment

To properly test the newly added k8s runtime class, create a test deployment:

```bash
cat <<EOF | tee nginx-urunc.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    run: nginx-urunc
  name: nginx-urunc
spec:
  replicas: 1
  selector:
    matchLabels:
      run: nginx-urunc
  template:
    metadata:
      labels:
        run: nginx-urunc
    spec:
      runtimeClassName: urunc
      containers:
      - image: harbor.nbfc.io/nubificus/urunc/nginx-firecracker-unikraft-initrd:latest
        imagePullPolicy: Always
        name: nginx-urunc
        ports:
        - containerPort: 80
          protocol: TCP
      restartPolicy: Always
---
apiVersion: v1
kind: Service
metadata:
  name: nginx-urunc
spec:
  ports:
  - port: 80
    protocol: TCP
    targetPort: 80
  selector:
    run: nginx-urunc
  sessionAffinity: None
  type: ClusterIP
EOF

kubectl apply -f nginx-urunc.yaml
```

Now, we should be able to see the created Pod:

```bash
kubectl get pods
```

## urunc-deploy

[`urunc-deploy`](https://github.com/urunc-dev/urunc/tree/main/deployment/urunc-deploy) provides a Dockerfile, which contains all of the binaries
and artifacts required to run `urunc`, as well as reference DaemonSets, which can
be utilized to install `urunc` runtime on a running Kubernetes cluster.


### urunc-deploy in k3s

To install in a k3s cluster, first we need to create the RBAC:

```bash
kubectl apply -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/urunc-rbac/urunc-rbac.yaml
```

Then, we create the `urunc-deploy` Daemonset, followed by the k3s customization.

The below command will install the latest version from the main branch. If you want to deploy a specific version, follow the instructions below this command.

```bash
kubectl apply -k https://github.com/urunc-dev/urunc//deployment/urunc-deploy/urunc-deploy/overlays/k3s?ref=main
```

If you want to deploy a specific version (e.g., `abc1234`), download the required files locally, set the version, update the image tag using `sed` and apply:

```bash
VERSION=abc1234
git clone https://github.com/urunc-dev/urunc.git && cd urunc
sed -i "s|ghcr.io/urunc-dev/urunc/urunc-deploy:latest|ghcr.io/urunc-dev/urunc/urunc-deploy:${VERSION}|" deployment/urunc-deploy/urunc-deploy/base/urunc-deploy.yaml
kubectl apply -k deployment/urunc-deploy/urunc-deploy/overlays/k3s
```

The k3s overlay mounts `/var/lib/rancher/k3s/agent/etc/containerd/`. If the
`urunc-deploy` Pod stays in `ContainerCreating` with a `FailedMount` event, k3s
keeps its data elsewhere (`--data-dir`); change the path in
`deployment/urunc-deploy/urunc-deploy/overlays/k3s/mount_k3s_conf.yaml`
accordingly.

Finally, we need to create the appropriate k8s runtime class:

```bash
kubectl apply -f https://raw.githubusercontent.com/urunc-dev/urunc/refs/heads/main/deployment/urunc-deploy/runtimeclasses/runtimeclass.yaml
```

To uninstall:

```bash
kubectl delete -k https://github.com/urunc-dev/urunc//deployment/urunc-deploy/urunc-deploy/overlays/k3s?ref=main
kubectl apply -k https://github.com/urunc-dev/urunc//deployment/urunc-deploy/urunc-cleanup/overlays/k3s?ref=main
```

Wait for the cleanup to finish on every node. The cleanup Pod restarts
`containerd`, removes the `urunc.io/urunc-runtime` label from its node, and
then exits on its own. The cleanup is complete once no node has the label
anymore, i.e. the following command prints `No resources found`:

```bash
kubectl get nodes -l urunc.io/urunc-runtime
```

On k3s, the cleanup restarts the k3s service instead of `containerd`. On server
nodes this briefly takes down the API server, so `kubectl` may report a
connection error for a few seconds; just retry the command.

> **Note:** Do not delete the cleanup DaemonSet before the label is gone.
> Doing so interrupts the restart, leaving any running `urunc` Pods
> stuck in `Terminating`.

Then remove the remaining resources:

```bash
kubectl delete -k https://github.com/urunc-dev/urunc//deployment/urunc-deploy/urunc-cleanup/overlays/k3s?ref=main
kubectl delete -f https://raw.githubusercontent.com/urunc-dev/urunc/refs/heads/main/deployment/urunc-deploy/runtimeclasses/runtimeclass.yaml
kubectl delete -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/urunc-rbac/urunc-rbac.yaml
```

### urunc-deploy in k8s with containerd

To install in a k8s cluster, first we need to create the RBAC:

```bash
kubectl apply -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/urunc-rbac/urunc-rbac.yaml
```

Then, we create the `urunc-deploy` Daemonset:

```bash
kubectl apply -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/urunc-deploy/base/urunc-deploy.yaml
```

Finally, we need to create the appropriate k8s runtime class:

```bash
kubectl apply -f https://raw.githubusercontent.com/urunc-dev/urunc/refs/heads/main/deployment/urunc-deploy/runtimeclasses/runtimeclass.yaml
```

To uninstall:

```bash
kubectl delete -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/urunc-deploy/base/urunc-deploy.yaml
kubectl apply -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/urunc-cleanup/base/urunc-cleanup.yaml
```

Wait for the cleanup to finish on every node. The cleanup Pod restarts
`containerd`, removes the `urunc.io/urunc-runtime` label from its node, and
then exits on its own. The cleanup is complete once no node has the label
anymore, i.e. the following command prints `No resources found`:

```bash
kubectl get nodes -l urunc.io/urunc-runtime
```

> **Note:** Do not delete the cleanup DaemonSet before that. Doing so
> interrupts the `containerd` restart, leaving any running `urunc` Pods
> stuck in `Terminating`.

Then remove the remaining resources:

```bash
kubectl delete -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/urunc-cleanup/base/urunc-cleanup.yaml
kubectl delete -f https://raw.githubusercontent.com/urunc-dev/urunc/refs/heads/main/deployment/urunc-deploy/runtimeclasses/runtimeclass.yaml
kubectl delete -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/urunc-rbac/urunc-rbac.yaml
```

Now, we can create new `urunc` deployments using the [instruction provided in manual installation](#create-a-test-deployment).

### How urunc-deploy works

`urunc-deploy` consists of several components and steps that install `urunc` along with the supported hypervisors,
configure `containerd` and Kubernetes (k8s) to use `urunc`, and provide a simple way to remove those components from the cluster.

The daemonset automatically installs all required artifacts under `/opt/urunc` and configures `urunc` via a configuration file at `/etc/urunc/config.toml`.

During installation, the following steps take place:

- A RBAC role is created to allow `urunc-deploy` to run with privileged access.
- The `urunc-deploy` Pod is deployed with privileges on the host, and the `containerd` configuration is mounted inside the Pod.
- `urunc-deploy` performs the following tasks:
    * Copies `urunc` and `containerd-shim-urunc-v2` binaries to the host under `/usr/local/bin`.
    * Copies hypervisor binaries to the host under `/opt/urunc/bin`.
    * Copies QEMU data files to `/opt/urunc/share`.
    * Installs a configuration file at `/etc/urunc/config.toml`.
    * Registers `urunc` as a `containerd` runtime: with containerd 2.x through a
      drop-in file, and with containerd 1.x or k0s by adding the runtime to the
      configuration file itself. The drop-in file goes into a directory the
      configuration already imports (`conf.d`, or `config-v3.toml.d` on
      k3s/rke2) if there is one, or else into `config.d`, with an import line
      added to the configuration. A missing configuration file is created. On
      k3s/rke2, which render `config.toml` on every start, the
      configuration file is their template (`config-v3.toml.tmpl`, or else
      `config.toml.tmpl`). A missing one is created as a copy of the rendered
      configuration, named `config-v3.toml.tmpl` with containerd 2.x.
      Do not edit the `urunc-deploy.toml` drop-in file: `urunc-deploy` rewrites
      it on every install and deletes it on uninstall.
    * Restarts `containerd`, if necessary.
    * Labels the Node with label `urunc.io/urunc-runtime=true`.
- Finally, `urunc` is added as a runtime class in k8s.

During cleanup, these changes are reverted:

- The `urunc` and `containerd-shim-urunc-v2` binaries are deleted from `/usr/local/bin`.
- The `/opt/urunc` directory containing hypervisor binaries and QEMU data files is deleted.
- The `/etc/urunc` configuration directory is deleted.
- The `urunc` changes are removed from the `containerd` configuration:
    * The `urunc-deploy` drop-in file is deleted.
    * A configuration file that `urunc-deploy` created is deleted, unless it was
      changed after the installation. In that case it is kept and only the
      `urunc` entries are removed from it.
    * In any other configuration file, only the `urunc` runtime, the
      `urunc-deploy` import and the debug level, if `urunc-deploy` set it for
      `DEBUG=true`, are removed; everything else, including changes made after
      the installation, is kept.
    * Directories created during the installation are removed if they are empty.
- `containerd` and the kubelet are restarted (on k3s/rke2, the k3s/rke2 service instead).
- The `urunc.io/urunc-runtime=true` label is removed from the Node.
- The RBAC role, the `urunc-deploy` Pod and the runtime class are removed.

> **Note:** Whenever `urunc-deploy` edits a `containerd` configuration file
> (to add the runtime or the import line, or to remove them during cleanup),
> the comments and formatting of that file are lost. With containerd 2.x,
> configurations that already import `conf.d` (or `config-v3.toml.d` on
> k3s/rke2) are never edited.

### Customizing the urunc configuration

`urunc-deploy` installs a default `config.toml`. Every value in it can be
overridden at deploy time via environment variables on the `urunc-deploy`
DaemonSet. A variable set to a non-empty value replaces the corresponding value
in the installed `/etc/urunc/config.toml`; unset or empty variables retain the
shipped default.

Each variable name is the TOML path of the setting, upper-cased, with `.` and `-`
replaced by `_`, and prefixed with `URUNC_`:

| Configuration key | Environment variable | Type |
| --- | --- | --- |
| `log.level` | `URUNC_LOG_LEVEL` | string |
| `log.syslog` | `URUNC_LOG_SYSLOG` | bool |
| `timestamps.enabled` | `URUNC_TIMESTAMPS_ENABLED` | bool |
| `timestamps.destination` | `URUNC_TIMESTAMPS_DESTINATION` | string |
| `runtime.libcontainer` | `URUNC_RUNTIME_LIBCONTAINER` | bool |
| `runtime.vAccel` | `URUNC_RUNTIME_VACCEL` | bool |
| `monitors.<monitor>.default_memory_mb` | `URUNC_MONITORS_<MONITOR>_DEFAULT_MEMORY_MB` | int |
| `monitors.<monitor>.default_vcpus` | `URUNC_MONITORS_<MONITOR>_DEFAULT_VCPUS` | int |
| `monitors.<monitor>.path` | `URUNC_MONITORS_<MONITOR>_PATH` | string |
| `monitors.qemu.data_path` | `URUNC_MONITORS_QEMU_DATA_PATH` | string |
| `monitors.qemu.vhost` | `URUNC_MONITORS_QEMU_VHOST` | bool |
| `monitors.<monitor>.socket_path` | `URUNC_MONITORS_<MONITOR>_SOCKET_PATH` | string |
| `extra_binaries.virtiofsd.path` | `URUNC_EXTRA_BINARIES_VIRTIOFSD_PATH` | string |
| `extra_binaries.virtiofsd.options` | `URUNC_EXTRA_BINARIES_VIRTIOFSD_OPTIONS` | string |

`<monitor>` is one of the monitors declared in `config.toml` (`qemu`,
`firecracker`, `cloud-hypervisor`, `spt`, `hvt`, `hyperlight-unikraft`), e.g.
`URUNC_MONITORS_HVT_DEFAULT_VCPUS`. Only the keys actually present for a monitor
in `config.toml` are overridable; `socket_path` is declared for `qemu`,
`firecracker` and `cloud-hypervisor`, the monitors that support a control socket.

Example — raise the default QEMU memory and enable debug logging:

```yaml
        env:
          - name: URUNC_LOG_LEVEL
            value: "debug"
          - name: URUNC_MONITORS_QEMU_DEFAULT_MEMORY_MB
            value: "1024"
```

Overrides are validated before any change is made to the host. An invalid
override aborts the installation with a descriptive error, leaving no urunc
artifacts, configuration, or `containerd` changes behind. The following are
rejected:

- a non-integer value, zero, or a value exceeding the maximum signed 64-bit integer,
  for an integer key (e.g. `*_DEFAULT_MEMORY_MB`, `*_DEFAULT_VCPUS`);
- a value other than `true` or `false` for a boolean key (e.g. `URUNC_LOG_SYSLOG`, `URUNC_TIMESTAMPS_ENABLED`);
- an unrecognised `URUNC_*` variable that does not map to a key in `config.toml` (e.g. a typo).

All detected problems are reported together. The `urunc-deploy` Pod logs contain
the corresponding `ERROR:` lines when an installation does not complete.
