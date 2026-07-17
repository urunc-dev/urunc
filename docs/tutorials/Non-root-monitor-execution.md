# Non-root execution of monitor

To enhance security, `urunc` supports running the monitor process (VMM or
seccomp monitor) as a non-root user. This can be as simple as setting the
respective uid/gid for the execution of the container.

## Running the monitor as non-root user

By default `urunc` will execute the monitor setting up the `uid`, `gid` and
`additionalGids` from the [container's OCI
configuration](https://github.com/opencontainers/runtime-spec/blob/main/config.md#posix-platform-user).
As a result, we simply need to instruct `urunc` to run a container as the desired
user.

### Docker and Nerdctl

In the case of docker and nerdctl, we can set the user and the groups of the
container with the `--user <uid>:<gid>` option and the additional groups using
`--group-add <gid>` for each additional group. Therefore, to run a KVM-enabled
monitor with `urunc` as `nobody`, we use the following command:

```bash
sudo nerdctl run  --user 65534:65534 --runtime "io.containerd.urunc.v2" --rm -it harbor.nbfc.io/nubificus/urunc/nginx-firecracker-unikraft-initrd:latest
```

> Note: The commands are the same for docker.


### In a k8s deployment

Similarly, in the case of Kubernetes, we can specify the monitor's process user
and groups by defining the container's user and groups. We can do that in
the `securityContext` field of the deployment yaml:

```
securityContext:
  runAsUser: 65534
  runAsGroup: 65534
  supplementalGroups: [1000]
```

For more information regarding the Security Context of a Pod / Container take a
look at [Kubernetes's
documentation](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/).
