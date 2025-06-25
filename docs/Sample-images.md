# Sample Unikernel OCI images

In this document, you can find the images used to perform `urunc`'s end-to-end tests.
This might be helpful for anyone looking to spawn some example unikernels using `urunc`.

The naming convention used for these images is $APPLICATION-$HYPERVISOR-$UNIKERNEL-$ADDITIONAL_INFO:tag
We plan to create and maintain multi-platform images soon, as well as enrich this list with new images.

- harbor.nbfc.io/nubificus/urunc/hello-hvt-rumprun-nonet:latest
- harbor.nbfc.io/nubificus/urunc/hello-hvt-rumprun:latest
- harbor.nbfc.io/nubificus/urunc/hello-hvt-mirage:latest
- harbor.nbfc.io/nubificus/urunc/hello-spt-mirage:latest
- harbor.nbfc.io/nubificus/urunc/hello-spt-rumprun-nonet:latest
- harbor.nbfc.io/nubificus/urunc/hello-spt-rumprun:latest
- harbor.nbfc.io/nubificus/urunc/hello-qemu-mewz:latest
- harbor.nbfc.io/nubificus/urunc/hello-qemu-unikraft:latest
- harbor.nbfc.io/nubificus/urunc/hello-world-qemu-linux-initrd:latest
- harbor.nbfc.io/nubificus/urunc/hello-firecracker-unikraft:latest
- harbor.nbfc.io/nubificus/urunc/hello-world-firecracker-linux-initrd:latest
- harbor.nbfc.io/nubificus/urunc/hello-env-qemu-unikraft-initrd:latest
- harbor.nbfc.io/nubificus/urunc/hello-env-qemu-linux-initrd:latest
- harbor.nbfc.io/nubificus/urunc/hello-env-firecracker-unikraft-initrd:latest
- harbor.nbfc.io/nubificus/urunc/hello-env-firecracker-linux-initrd:latest
- harbor.nbfc.io/nubificus/urunc/nginx-qemu-unikraft-initrd:latest
- harbor.nbfc.io/nubificus/urunc/nginx-qemu-linux-raw:latest
- harbor.nbfc.io/nubificus/urunc/nginx-qemu-linux-block:latest
- harbor.nbfc.io/nubificus/urunc/nginx-hvt-rumprun-block:latest
- harbor.nbfc.io/nubificus/urunc/nginx-spt-rumprun-block:latest
- harbor.nbfc.io/nubificus/urunc/nginx-firecracker-unikraft-initrd:latest
- harbor.nbfc.io/nubificus/urunc/nginx-firecracker-linux-raw:latest
- harbor.nbfc.io/nubificus/urunc/nginx-firecracker-linux-block:latest
- harbor.nbfc.io/nubificus/urunc/hello-server-qemu-mewz:latest
- harbor.nbfc.io/nubificus/urunc/httpreply-firecracker-unikraft:latest
- harbor.nbfc.io/nubificus/urunc/redis-hvt-rumprun-raw:latest
- harbor.nbfc.io/nubificus/urunc/redis-spt-rumprun-raw:latest
- harbor.nbfc.io/nubificus/urunc/redis-hvt-rumprun-block:latest
- harbor.nbfc.io/nubificus/urunc/redis-spt-rumprun-block:latest
- harbor.nbfc.io/nubificus/urunc/redis-qemu-linux-raw:latest
- harbor.nbfc.io/nubificus/urunc/redis-qemu-unikraft-initrd:latest
- harbor.nbfc.io/nubificus/urunc/redis-qemu-linux-block:latest
- harbor.nbfc.io/nubificus/urunc/redis-firecracker-linux-raw:latest
- harbor.nbfc.io/nubificus/urunc/redis-firecracker-linux-block:latest
- harbor.nbfc.io/nubificus/urunc/net-hvt-mirage:latest
- harbor.nbfc.io/nubificus/urunc/net-spt-mirage:latest
- harbor.nbfc.io/nubificus/urunc/net-qemu-mirage:latest
- harbor.nbfc.io/nubificus/urunc/block-test-hvt-mirage:latest
- harbor.nbfc.io/nubificus/urunc/block-test-spt-mirage:latest
- harbor.nbfc.io/nubificus/urunc/whoami-qemu-linux-initrd:latest
- harbor.nbfc.io/nubificus/urunc/whoami-firecracker-linux-initrd:latest
