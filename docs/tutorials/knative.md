# Knative + urunc: Deploying Serverless Unikernels

This guide walks you through deploying [Knative Serving](https://knative.dev/)
using [`urunc`](https://github.com/urunc-dev/urunc). You’ll build Knative from
a custom branch and use [`ko`](https://github.com/ko-build/ko) for seamless
image building and deployment.

## Prerequisites

-   A running Kubernetes cluster
-   A Docker-compatible registry (e.g. Harbor, Docker Hub)
-   Ubuntu 20.04 or newer
-   Basic `git`, `curl`, `kubectl`, and `docker` installed
    
## Environment Setup

Install [Docker](/quickstart/#install-docker), Go >= 1.21, and `ko`:

### Install Go 1.21  
```bash
sudo mkdir /usr/local/go1.21
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
sudo tar -zxvf go1.21.5.linux-amd64.tar.gz -C /usr/local/go1.21/
rm go1.21.5.linux-amd64.tar.gz
```

### Verify Go installation (Should be 1.21.5)

```console
$ export GOROOT=/usr/local/go1.21/go 
$ export PATH=$GOROOT/bin:$PATH  
$ export GOPATH=$HOME/go 
$ go version
go version go1.21.5 linux/amd64
```

### Install ko VERSION=0.15.1
```bash
export OS=Linux
export ARCH=x86_64
curl -sSfL "https://github.com/ko-build/ko/releases/download/v${VERSION}/ko_${VERSION}_${OS}_${ARCH}.tar.gz" -o ko.tar.gz
sudo tar -zxvf ko.tar.gz -C /usr/local/bin` 
```

## Clone and Build Knative with the queue-proxy patch

### Set your container registry  

> Note: You should be able to use dockerhub for this. e.g. `<yourdockerhubid>/knative`

```bash
export KO_DOCKER_REPO='harbor.nbfc.io/nubificus/knative-install-urunc'
```

### Clone urunc-enabled Knative Serving 
```bash
git clone https://github.com/nubificus/serving -b feat_urunc 
cd serving/
ko resolve -Rf ./config/core/ > knative-custom.yaml
```

### Apply knative's manifests to the local k8s
```bash
kubectl apply -f knative-custom.yaml
```

Alternatively, you could use our latest build:
```bash
kubectl apply -f https://s3.nbfc.io/knative/knative-v[[ versions.knative ]]-urunc-5220308.yaml
```

> Note: There are cases where due to the large manifests, kubectl fails. Try a second time, or use `kubectl create -f https://s3.nbfc.io/knative/knative-v[[ versions.knative ]]-urunc-5220308.yaml`

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

### Test the service (replace IP with actual ingress IP) 

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

and once it's on a `Ready` state, you could issue a request:
> Note: 10.244.9.220 is the IP of the `kourier-internal` svc. You can check your own from:
> `kubectl get svc -n kourier-system |grep kourier-internal`

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

## Wrapping Up

You're now running unikernel-based workloads via Knative and `urunc`! With this
setup, you can push the boundaries of lightweight, secure, and high-performance
serverless deployments — all within a Kubernetes-native environment.
