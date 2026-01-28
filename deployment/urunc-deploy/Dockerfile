# Copyright (c) 2023-2026, Nubificus LTD
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM debian:bullseye@sha256:25c0cab214b810db1b3c8adef5a12a92596979abddf86bb364e8d9c9d111df9f AS solo5-builder

# Remove libc-bin files to avoid segmentation fault.
# See more: https://stackoverflow.com/questions/78105004/docker-build-fails-because-unable-to-install-libc-bins
RUN rm -f /var/lib/dpkg/info/libc-bin.* && \
    apt-get clean && \
    apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    libc-bin build-essential libseccomp-dev git pkg-config && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Clone Solo5 repository and force static linking
RUN git -c http.sslVerify=false clone --branch v0.9.3 https://github.com/Solo5/solo5.git . && \
    sed -i '66 a\HOSTLDFLAGS += -static-pie' tenders/GNUmakefile

# Build Solo5 and verify artifacts
RUN ./configure.sh && make || true && \
    test -f tenders/spt/solo5-spt && test -f tenders/hvt/solo5-hvt

# Copy artifacts to a separate directory
WORKDIR /artifacts
RUN cp /app/tenders/hvt/solo5-hvt /artifacts/ && \
    cp /app/tenders/spt/solo5-spt /artifacts/

FROM golang:1.24.6-alpine3.21@sha256:50f8a10a46c0c26b5b816a80314f1999196c44c3e3571f41026b061339c29db6 AS urunc-builder
RUN apk update && \
    apk add --no-cache git make build-base linux-headers
WORKDIR /app
ARG REPO=urunc-dev/urunc
ARG BRANCH=main
RUN git clone --depth 1 --branch ${BRANCH} https://github.com/${REPO} . && \
    make
WORKDIR /artifacts
RUN cp -r /app/dist .

FROM alpine:3.21@sha256:b6a6be0ff92ab6db8acd94f5d1b7a6c2f0f5d10ce3c24af348d333ac6da80685 AS firecracker-builder
RUN apk update && apk add --no-cache curl tar
RUN ARCH=$(uname -m) && \
    VERSION="v1.7.0" && \
    RELEASE_URL="https://github.com/firecracker-microvm/firecracker/releases" && \
    curl -L -o firecracker.tgz ${RELEASE_URL}/download/${VERSION}/firecracker-${VERSION}-${ARCH}.tgz && \
    tar xzf firecracker.tgz && \
    mv release-${VERSION}-${ARCH}/firecracker-${VERSION}-${ARCH} firecracker
WORKDIR /artifacts
RUN cp /firecracker .

FROM quay.io/kata-containers/kata-deploy:3.15.0@sha256:fbab8bdbe910b52cf9396261f887f894cd3e71f86abe68ec11595ab7628b4a10 AS qemu-builder
WORKDIR /artifacts
RUN ARCH=$(uname -m) && \
    cp /opt/kata-artifacts/opt/kata/bin/qemu-system-${ARCH} ./qemu-system


FROM alpine:3.21@sha256:b6a6be0ff92ab6db8acd94f5d1b7a6c2f0f5d10ce3c24af348d333ac6da80685 AS intermediate

COPY --from=solo5-builder /artifacts /urunc-artifacts/hypervisors
COPY --from=qemu-builder /artifacts /urunc-artifacts/hypervisors
COPY --from=qemu-builder /opt/kata-artifacts/opt/kata/share/kata-qemu/qemu /urunc-artifacts/opt/kata/share/kata-qemu/qemu
COPY --from=qemu-builder /opt/kata-artifacts/opt/kata/libexec/virtiofsd /urunc-artifacts/libexec/virtiofsd
COPY --from=firecracker-builder /artifacts /urunc-artifacts/hypervisors
COPY --from=urunc-builder /artifacts/dist /urunc-artifacts

RUN apk update && \
    apk add --no-cache curl && \
    ARCH=$(uname -m) && \
    mv /urunc-artifacts/hypervisors/qemu-system /urunc-artifacts/hypervisors/qemu-system-${ARCH}  && \
    if [ "${ARCH}" = "x86_64" ]; then ARCH=amd64; fi && \
    if [ "${ARCH}" = "aarch64" ]; then ARCH=arm64; fi && \
    mv /urunc-artifacts/containerd-shim-urunc-v2_static_${ARCH} /urunc-artifacts/containerd-shim-urunc-v2 && \
    mv /urunc-artifacts/urunc_static_${ARCH} /urunc-artifacts/urunc && \
    KUBECTL_VERSION=$(curl -L -s https://dl.k8s.io/release/stable.txt) && \
    curl -fL --progress-bar -o /usr/bin/kubectl https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl && \
    chmod +x /usr/bin/kubectl && \
    curl -fL --progress-bar -o /usr/bin/jq https://github.com/jqlang/jq/releases/download/jq-1.7.1/jq-linux-${ARCH} && \
    chmod +x /usr/bin/jq

# Final image
FROM alpine:3.21@sha256:b6a6be0ff92ab6db8acd94f5d1b7a6c2f0f5d10ce3c24af348d333ac6da80685
COPY --from=intermediate /urunc-artifacts /urunc-artifacts
COPY --from=intermediate /usr/bin/jq /usr/bin/jq
COPY --from=intermediate /usr/bin/kubectl /usr/bin/kubectl
COPY scripts/install.sh /urunc-artifacts/scripts/install.sh
COPY config.toml /deployment/config.toml
RUN apk update && \
    apk add --no-cache bash curl py3-pip && \
    pip install --no-cache-dir --break-system-packages yq==3.2.3 && \
    chmod +x /urunc-artifacts/scripts/install.sh
