# Redis Unikernel Example

This example demonstrates how to build and package a Redis unikernel using `kraft` and `bunny`.

One of the great features of `bunny` is that it can dynamically create the `initrd` (root filesystem) for you. Instead of dealing with complex build-system hacks to embed `redis.conf`, we can simply tell `bunny` to include our local `redis.conf` in the final image!

## Step 1: Compile the Unikernel

First, clone the Unikraft Redis repository and build the binary:

```bash
git clone https://github.com/unikraft/app-redis.git
cd app-redis
kraft build --target qemu-x86_64
```

> [!WARNING]
> The `kraft build` step MUST succeed and produce a valid kernel binary. `bunny` only packages existing files; if the compilation fails or produces an empty file, the resulting OCI image will not be able to boot in `urunc`.

## Step 2: Package with Bunny

Copy the `bunnyfile` from this directory into your `app-redis` project directory, along with the kernel. Create a default `redis.conf` file as well.

For example:
```bash
cp /path/to/urunc/examples/redis/bunnyfile .
cp .unikraft/build/redis_qemu-x86_64 ./redis-qemu-x86_64

# Create a basic redis.conf
echo "port 6379" > redis.conf
```

Notice the `rootfs` section in our `bunnyfile`:
```yaml
rootfs:
  from: scratch
  type: initrd
  include:
  - redis.conf:/conf/redis.conf
```
This instructs `bunny` to generate a new `initrd` from scratch and embed our local `redis.conf` into `/conf/redis.conf`.

Finally, build the OCI image:

```bash
docker build -f bunnyfile -t urunc-redis:latest .
```

You now have a fully-packaged Redis unikernel image ready to be run with `urunc`!
