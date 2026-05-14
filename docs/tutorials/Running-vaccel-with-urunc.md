# Running vAccel-enabled Containers with `urunc`
In this tutorial, we describe how to run vAccel-enabled Linux containers 
with `urunc` using **QEMU** or **Firecracker** as the underlying
hypervisor.

## **QEMU**
When running under QEMU, communication between the guest and the host agent is established 
using vsock. The RPC agent listens on a vsock address on the host, and the guest connects to 
it using the same address.

### Host side
Start the vAccel RPC agent on the host and configure it to listen on a vsock address:
```bash
export VACCEL_PLUGINS=libvaccel-noop.so
export VACCEL_LOG_LEVEL=4
vaccel-rpc-agent -a vsock://2:2049
```
This spawns the RPC agent and makes it available to guests via vsock port `2049`.

### Guest side
Run the container image with the appropriate runtime annotations:
```bash 
sudo docker run --runtime io.containerd.urunc.v2 --rm -it --annotation com.urunc.unikernel.vAccel="vsock" --annotation com.urunc.unikernel.RPCAddress="vsock://2:2049" --pull always harbor.nbfc.io/nubificus/ubuntu-vaccel-urunc-qemu:x86_64
```
Inside the container, execute an example vAccel workload:
```bash 
classify /usr/share/vaccel/images/example.jpg 1
```

### Expected output
```bash
2025.12.11-15:02:16.45 - <info> vAccel 0.7.1-51-499fc2f7
2025.12.11-15:02:16.50 - <info> Registered plugin rpc 0.2.1-10-3d4d748c
Initialized session with id: 1
classification tags: This is a dummy classification tag!
classification imagename: This is a dummy imgname!
```
If you rerun the command with a higher log level:
```bash
export VACCEL_LOG_LEVEL=4
classify /usr/share/vaccel/images/example.jpg 1
```
The expected output is:
```bash
2025.12.11-15:03:04.72 - <debug> Initializing vAccel
2025.12.11-15:03:04.72 - <info> vAccel 0.7.1-51-499fc2f7
2025.12.11-15:03:04.72 - <debug> Config:
2025.12.11-15:03:04.72 - <debug>   plugins = libvaccel-rpc.so
2025.12.11-15:03:04.72 - <debug>   log_level = debug
2025.12.11-15:03:04.72 - <debug>   log_file = (null)
2025.12.11-15:03:04.72 - <debug>   profiling_enabled = false
2025.12.11-15:03:04.72 - <debug>   version_ignore = false
2025.12.11-15:03:04.74 - <debug> Created top-level rundir: /run/user/0/vaccel/oMcLeO
2025.12.11-15:03:04.75 - <info> Registered plugin rpc 0.2.1-10-3d4d748c
2025.12.11-15:03:04.76 - <debug> rpc is a VirtIO module
2025.12.11-15:03:04.76 - <debug> Registered op exec from plugin rpc
2025.12.11-15:03:04.77 - <debug> Registered op exec_with_resource from plugin rpc
2025.12.11-15:03:04.77 - <debug> Registered op image_classify from plugin rpc
2025.12.11-15:03:04.77 - <debug> Registered op image_detect from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op image_segment from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op image_depth from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op image_pose from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op tflite_model_load from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op tflite_model_unload from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op tflite_model_run from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op torch_model_load from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op torch_model_run from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op blas_sgemm from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op fpga_arraycopy from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op fpga_mmult from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op fpga_parallel from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op fpga_vectoradd from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op minmax from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op opencv from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op tf_model_load from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op tf_model_unload from plugin rpc
2025.12.11-15:03:04.78 - <debug> Registered op tf_model_run from plugin rpc
2025.12.11-15:03:04.78 - <debug> Loaded plugin rpc from libvaccel-rpc.so
2025.12.11-15:03:04.79 - <debug> [rpc] Initializing new remote session
2025.12.11-15:03:04.86 - <debug> [rpc] Initialized remote session 2
2025.12.11-15:03:04.86 - <debug> New rundir for session 1: /run/user/0/vaccel/oMcLe1
2025.12.11-15:03:04.87 - <debug> Initialized session 1 with plugin rpc (remote id: )
Initialized session with id: 1
2025.12.11-15:03:04.88 - <debug> session:1 Looking for func implementing op image_cy
2025.12.11-15:03:04.88 - <debug> Returning func for op image_classify from plugin rc
2025.12.11-15:03:04.89 - <debug> [rpc] session:2 Executing op image_classify
classification tags: This is a dummy classification tag!
classification imagename: This is a dummy imgname!
2025.12.11-15:03:04.90 - <debug> [rpc] Releasing remote session 2
2025.12.11-15:03:04.91 - <debug> Released session 1
2025.12.11-15:03:04.91 - <debug> Cleaning up vAccel
2025.12.11-15:03:04.91 - <debug> Cleaning up sessions
2025.12.11-15:03:04.92 - <debug> Cleaning up resources
2025.12.11-15:03:04.92 - <debug> Cleaning up plugins
2025.12.11-15:03:04.93 - <debug> Unregistered plugin rpc
```
## **Firecracker**
For Firecracker, the guest still uses vsock, but the host-side agent listens on a unix 
socket instead. urunc automatically translates the unix socket address into a vsock 
address and bind-mounts the socket path into the guest.

### Host side
Start the RPC agent using a unix socket:
```bash
export VACCEL_PLUGINS=libvaccel-noop.so
export VACCEL_LOG_LEVEL=4
sudo mkdir /vaccel  # if does not exist
sudo chown <user> /vaccel
vaccel-rpc-agent -a unix:///vaccel/vaccel.sock_2049
```
The socket directory must be accessible so it can be bind-mounted into the guest.

### Guest side
Run the container using `nerdctl` and the `devmapper` snapshotter:
```bash 
sudo nerdctl run --runtime io.containerd.urunc.v2 --rm -it --snapshotter devmapper --annotation com.urunc.unikernel.vAccel="vsock" --annotation com.urunc.unikernel.RPCAddress="unix:///vaccel/vaccel.sock_2049" --pull always harbor.nbfc.io/nubificus/ubuntu-vaccel-urunc-fc:x86_64
```

Run the same example workload:
```bash
classify /usr/share/vaccel/images/example.jpg 1
```

### Expected output
The expected output is identical to the QEMU execution.