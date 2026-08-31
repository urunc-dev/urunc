# Knative + urunc: Deploying Serverless Unikernels

This guide walks you through deploying [Knative Serving](https://knative.dev/)
with urunc support on Kubernetes. We provide pre-built binaries for quick setup,
or you can build Knative from source using [`ko`](https://github.com/ko-build/ko)
for custom configurations.

## Prerequisites

-   A running Kubernetes cluster
-   `urunc` installed (follow the [installation guide](https://urunc.io/tutorials/How-to-urunc-on-k8s/))

## Install Knative Serving

### Option 1: Use Pre-built Knative (Recommended)

Apply the pre-built Knative manifests with urunc support:

```bash
kubectl apply -f https://s3.nbfc.io/knative/knative-v[[ versions.knative ]]-urunc-5220308.yaml
```

> Note: There are cases where due to the large manifests, kubectl fails. Try a second time, or use `kubectl create -f https://s3.nbfc.io/knative/knative-v[[ versions.knative ]]-urunc-5220308.yaml`

### Option 2: Build Knative from Source

If you prefer to build Knative yourself, follow these steps.

#### Prerequisites for building from source

-   A Docker-compatible registry (e.g. Harbor, Docker Hub)
-   Ubuntu 20.04 or newer
-   Basic `git`, `curl`, `kubectl`, and `docker` installed

#### Environment Setup

Install [Docker](/quickstart/#install-docker), Go [[ versions.go ]], and `ko`:

##### Install Go (≥ 1.23)

Please follow the official [Go installation instructions](https://go.dev/doc/install) to install Go 1.23 or newer.

##### Verify Go installation

```console
$ export PATH=/usr/local/go/bin:$PATH  
$ export GOPATH=$HOME/go 
$ go version
```

##### Install ko
```bash
curl -sSfL https://github.com/ko-build/ko/releases/latest/download/ko_Linux_x86_64.tar.gz | sudo tar xzf - -C /usr/local/bin
ko version
```

#### Set your container registry  

> Note: You should be able to use Docker Hub for this. e.g. `<yourdockerhubid>/knative`

```bash
export KO_DOCKER_REPO='<your-registry>/knative'
```

#### Clone urunc-enabled Knative Serving and build

```bash
git clone https://github.com/nubificus/serving -b feat_urunc
cd serving/
ko resolve -Rf ./config/core/ > knative-custom.yaml
```

#### Apply knative's manifests to the local k8s

```bash
kubectl apply -f knative-custom.yaml
```

## Setup Networking (Kourier)

### Install kourier, patch ingress and domain configs

```bash
kubectl apply -f https://github.com/knative/net-kourier/releases/latest/download/kourier.yaml 
kubectl patch configmap/config-network -n knative-serving --type merge -p \ 
  '{"data":{"ingress.class":"kourier.ingress.networking.knative.dev"}}'
kubectl patch configmap/config-domain -n knative-serving --type merge -p \ 
  '{"data":{"127.0.0.1.nip.io":""}}'
```

## Enable RuntimeClass and urunc Support


### Install `urunc`

You can follow the documentation to install `urunc` from: [Installing](https://urunc.io/tutorials/How-to-urunc-on-k8s/)

### Enable runtimeClass for services, nodeSelector and affinity

```bash
kubectl patch configmap/config-features --namespace knative-serving --type merge --patch '{"data":{
  "kubernetes.podspec-affinity":"enabled",
  "kubernetes.podspec-runtimeclassname":"enabled",
  "kubernetes.podspec-nodeselector":"enabled"
}}'
```

## Deploy a Sample urunc Service

```bash
kubectl get ksvc -A -o wide
```

Should be empty. Create an simple httpreply
[service](https://github.com/nubificus/c-httpreply/blob/main/service.yaml),
based on a [simple C program](https://github.com/nubificus/c-httpreply):

```bash
kubectl apply -f https://raw.githubusercontent.com/nubificus/c-httpreply/refs/heads/main/service.yaml
```

### Check Knative Service 
 
```bash
kubectl get ksvc -A -o wide 
```

### Get the ingress IP

Before testing the service, get the IP address of the Kourier internal service:

```bash
kubectl get svc -n kourier-system kourier-internal -o jsonpath='{.spec.clusterIP}'
```

Note the IP address returned (e.g., `10.244.9.220`). You'll use this in the following curl commands.

### Test the service

Replace `<INGRESS_IP>` with the IP address from the previous step:

```bash
curl -v -H "Host: hellocontainerc.default.127.0.0.1.nip.io" http://<INGRESS_IP>
```

Now, let's create a `urunc`-compatible function. Create a [service](https://github.com/nubificus/app-httpreply/blob/fb0ec5c7f5e6b1fedbc589cdc96477c472fef2ca/service.yaml), based on Unikraft's [httpreply example](https://github.com/nubificus/app-httpreply/tree/feat_generic): 

```bash
kubectl apply -f https://raw.githubusercontent.com/nubificus/app-httpreply/refs/heads/feat_generic/service.yaml
```

You should be able to see this being created:

```console
$ kubectl get ksvc -o wide
NAME             URL                                                  LATESTCREATED              LATESTREADY                READY   REASON
hellounikernelfc http://hellounikernelfc.default.127.0.0.1.nip.io     hellounikernelfc-00001     hellounikernelfc-00001     True
```

Once it's in a `Ready` state, invoke the function using the ingress IP you obtained earlier:

```console
$ curl -v -H "Host: hellounikernelfc.default.127.0.0.1.nip.io" http://10.244.9.220:80
*   Trying 10.244.9.220:80...
* Connected to 10.244.9.220 (10.244.9.220) port 80 (#0)
> GET / HTTP/1.1
> Host: hellounikernelfc.default.127.0.0.1.nip.io
> User-Agent: curl/7.81.0
> Accept: */*
>
* Mark bundle as not supporting multiuse
< HTTP/1.1 200 OK
< content-length: 14
< content-type: text/html; charset=UTF-8
< date: Tue, 08 Apr 2025 15:47:45 GMT
< x-envoy-upstream-service-time: 774
< server: envoy
<
Hello, World!
* Connection #0 to host 10.244.9.220 left intact
```
