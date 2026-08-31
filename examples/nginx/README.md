# Nginx Unikernel Example

This example demonstrates how to build and package an Nginx unikernel using `kraft` and `bunny`.

## Step 1: Compile the Unikernel

First, clone the Unikraft Nginx repository and build the binary and rootfs:

```bash
git clone https://github.com/unikraft/app-nginx.git
cd app-nginx
kraft build --target qemu-x86_64
```

> [!WARNING]
> The `kraft build` step MUST succeed and produce a valid kernel binary and rootfs. `bunny` only packages existing files; if the compilation fails or produces an empty file, the resulting OCI image will not be able to boot in `urunc`.

After a successful build, you will have the compiled kernel and a root filesystem archive in the output directory.

## Step 2: Package with Bunny

Copy the `bunnyfile` from this directory into your `app-nginx` project directory, along with the kernel and rootfs so they match the paths in the `bunnyfile`.

For example:
```bash
cp /path/to/urunc/examples/nginx/bunnyfile .
cp .unikraft/build/nginx_qemu-x86_64 ./nginx-qemu-x86_64
cp .unikraft/build/initrd.cpio ./rootfs.cpio
```

Then, build the OCI image using Docker and the `bunny` BuildKit frontend:

```bash
docker build -f bunnyfile -t urunc-nginx:latest .
```

You now have a fully-packaged Nginx unikernel image ready to be run with `urunc`!
