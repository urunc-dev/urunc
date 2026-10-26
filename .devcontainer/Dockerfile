# Copyright (c) 2023-2026, Nubificus LTD
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV GO_VERSION=1.26.4

RUN apt-get update && apt-get install -y \
    curl \
    wget \
    git \
    make \
    gcc \
    qemu-kvm \
    qemu-utils \
    bridge-utils \
    iptables \
    net-tools \
    iproute2 \
    && rm -rf /var/lib/apt/lists/*

RUN wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz \
    && rm go${GO_VERSION}.linux-amd64.tar.gz
ENV PATH=$PATH:/usr/local/go/bin

RUN apt-get update && apt-get install -y \
    containerd \
    containernetworking-plugins \
    && rm -rf /var/lib/apt/lists/*

# NOTE: Firecracker, Solo5, and Cloud-Hypervisor are not pre-installed.
# These are required only for e2e tests that spawn VMs. Users can install
# them manually, or use the local VS Code + Docker path with --device=/dev/kvm
# for full e2e testing. The dev container supports make unittest and make lint
# out of the box.

WORKDIR /workspace

