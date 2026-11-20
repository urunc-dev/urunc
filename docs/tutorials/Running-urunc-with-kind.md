# Running urunc with kind (Kubernetes in Docker)

This tutorial sets up `urunc` in a `kind` (Kubernetes in Docker) cluster and
deploys a test NGINX unikernel.

## Prerequisites

- **Tools**: Docker Engine, `kind` and `kubectl`.
- **Host**: An x86_64 host with KVM. The NGINX unikernel of this tutorial is
  built for x86_64 and runs on Qemu with KVM.

## Overview

`kind` runs each Kubernetes node as a Docker container. This tutorial creates a
cluster with a single node, which has access to the KVM device of the host.
Then, `urunc-deploy` installs `urunc` and its monitors in the node and adds
`urunc` as a runtime of `containerd`. Finally, an NGINX unikernel runs as a Pod
with the `urunc` RuntimeClass and replies to a request from the host.

## Steps

### Step 0: Check KVM support

Check that the host provides `/dev/kvm`:

```bash
ls -l /dev/kvm
```

If the device does not exist, enable virtualization in the BIOS and load the
`kvm_intel` or `kvm_amd` kernel module.

### Step 1: Create a kind cluster with KVM access

1. **Delete any existing cluster with the same name**:

    ```bash
    kind delete cluster --name urunc-test
    ```

2. **Create kind-config.yaml**:

    ```bash
    cat <<EOF | tee kind-config.yaml
    kind: Cluster
    apiVersion: kind.x-k8s.io/v1alpha4
    nodes:
      - role: control-plane
        extraMounts:
          - hostPath: /dev/kvm
            containerPath: /dev/kvm
        extraPortMappings:
          - containerPort: 30080
            hostPort: 8080
            listenAddress: "127.0.0.1"
            protocol: TCP
    EOF
    ```

    The mount gives the node access to KVM. The node runs in a Docker
    container, so the host cannot reach its Pods directly. Therefore, the port
    mapping exposes port `30080` of the node as port `8080` of the host.

3. **Create the cluster**:

    ```bash
    kind create cluster --name urunc-test --config kind-config.yaml
    ```

4. **Verify KVM access**:

    ```bash
    docker exec urunc-test-control-plane ls /dev/kvm
    ```

    The output should be `/dev/kvm`.

### Step 2: Install urunc with urunc-deploy

[`urunc-deploy`](https://github.com/urunc-dev/urunc/tree/main/deployment/urunc-deploy)
is a DaemonSet that installs `urunc` and the supported monitors in each node.
It also adds `urunc` as a runtime in the `containerd` configuration of the node
and restarts `containerd`. More details are in the [urunc-deploy
section](How-to-urunc-on-k8s.md#urunc-deploy) of the Kubernetes tutorial.

1. **Create the RBAC of urunc-deploy**:

    ```bash
    kubectl apply -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/urunc-rbac/urunc-rbac.yaml
    ```

2. **Create the urunc-deploy DaemonSet**:

    ```bash
    kubectl apply -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/urunc-deploy/base/urunc-deploy.yaml
    ```

3. **Wait for the installation to finish**:

    When the installation finishes, `urunc-deploy` labels the node with
    `urunc.io/urunc-runtime=true`:

    ```bash
    kubectl wait --for=jsonpath='{.metadata.labels.urunc\.io/urunc-runtime}'=true node/urunc-test-control-plane --timeout=300s
    ```

4. **Create the urunc RuntimeClass**:

    ```bash
    kubectl apply -f https://raw.githubusercontent.com/urunc-dev/urunc/main/deployment/urunc-deploy/runtimeclasses/runtimeclass.yaml
    ```

### Step 3: Deploy the NGINX unikernel

1. **Create the Pod**:

    ```bash
    cat <<EOF | tee nginx-urunc.yaml
    apiVersion: v1
    kind: Pod
    metadata:
      name: nginx-urunc
      labels:
        run: nginx-urunc
    spec:
      runtimeClassName: urunc
      containers:
        - name: nginx
          image: harbor.nbfc.io/nubificus/urunc/nginx-qemu-unikraft-initrd:latest
          imagePullPolicy: Always
          ports:
            - containerPort: 80
              protocol: TCP
          resources:
            requests:
              cpu: 10m
    EOF
    kubectl apply -f nginx-urunc.yaml
    ```

2. **Expose NGINX with a `NodePort` Service**:

    The `nodePort` must match the `containerPort` of the port mapping in
    Step 1.

    ```bash
    cat <<EOF | tee nginx-urunc-svc.yaml
    apiVersion: v1
    kind: Service
    metadata:
      name: nginx-urunc
    spec:
      type: NodePort
      selector:
        run: nginx-urunc
      ports:
        - port: 80
          targetPort: 80
          nodePort: 30080
    EOF
    kubectl apply -f nginx-urunc-svc.yaml
    ```

### Step 4: Verify the deployment

1. **Wait for the Pod**:

    ```bash
    kubectl wait --for=condition=Ready pod/nginx-urunc --timeout=300s
    ```

    The output should be `pod/nginx-urunc condition met`.

2. **Check the logs**:

    ```bash
    kubectl logs nginx-urunc
    ```

    The logs of Unikraft should show NGINX starting without errors. The
    unikernel needs a few seconds to boot, so run the command again if the
    output is empty. The output should end with lines similar to:

    ```console
    Powered by
    o.   .o       _ _               __ _
    Oo   Oo  ___ (_) | __ __  __ _ ' _) :_
    oO   oO ' _ `| | |/ /  _)' _` | |_|  _)
    oOo oOO| | | | |   (| | | (_) |  _) :_
     OoOoO ._, ._:_:_,\_._,  .__,_:_, \___)
          Epimetheus 0.12.0~4c7352c0-custom
    [    1.690891] Info: [libukboot] <boot.c @  348> Pre-init table at 0x29f0d8 - 0x29f0d8
    [    1.722938] Info: [libukboot] <boot.c @  359> Constructor table at 0x29f0d8 - 0x29f0d8
    [    1.751149] Info: [libukboot] <boot.c @  369> Calling main(3, ['/cntrRootfs/unikernel/app-nginx_kvm-x86_64', '-c', '/nginx/conf/nginx.conf'])
    ```

3. **Send a request to NGINX**:

    The request goes through the port mapping of Step 1:

    ```bash
    curl http://127.0.0.1:8080
    ```

    The output should be the page that NGINX serves:

    ```html
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

### Step 5: Clean up

Delete the cluster, along with `urunc` and the NGINX Pod:

```bash
kind delete cluster --name urunc-test
```
