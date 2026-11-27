# Httpreply Unikernel Example

This example demonstrates how to build and package a simple HTTP reply unikernel using `kraft` and `bunny`.

## Step 1: Compile the Unikernel

First, clone the Unikraft httpreply repository (or a similar simple web server example) and build the binary:

```bash
git clone https://github.com/unikraft/app-httpreply.git
cd app-httpreply
kraft build --target qemu-x86_64
```

> [!WARNING]
> The `kraft build` step MUST succeed and produce a valid kernel binary. `bunny` only packages existing files; if the compilation fails or produces an empty file, the resulting OCI image will not be able to boot in `urunc`.

## Step 2: Package with Bunny

Copy the `bunnyfile` from this directory into your project directory, along with the kernel. 
Since `httpreply` is a simple unikernel, it generally does not require a complex root filesystem, so our `bunnyfile` just uses `scratch`.

For example:
```bash
cp /path/to/urunc/examples/httpreply/bunnyfile .
cp .unikraft/build/httpreply_qemu-x86_64 ./httpreply-qemu-x86_64
```

Then, build the OCI image using Docker and the `bunny` BuildKit frontend:

```bash
docker build -f bunnyfile -t urunc-httpreply:latest .
```

You now have a fully-packaged HTTP unikernel image ready to be run with `urunc`!
