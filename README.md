# urunc

[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/10840/badge)](https://www.bestpractices.dev/projects/10840)

Welcome to `urunc`, the "runc for unikernels".

## Table of Contents

1. [Introduction](#introduction)
2. [Quick start](#quick-start)
3. [Installation guide](#installation-guide)
4. [Documentation](#documentation)
5. [Supported platforms](#supported-platforms)
6. [Publications and talks](#publications-and-talks)
7. [Bug Reporting](#bug-reporting)
8. [Community & Meetings](#community-and-meetings)
9. [Roadmap](#roadmap)
10. [Contributing](#Contributing)
11. [Security Policy](#security-policy)
12. [Changelog](#changelog)
13. [License](#Introduction)
14. [Contact](#Introduction)

## Introduction

The main goal of `urunc` is to bridge the gap between traditional unikernels
and containerized environments, enabling seamless integration with cloud-native
architectures. Designed to fully leverage the container semantics and benefits
from the OCI tools and methodology, `urunc` aims to become “runc for
unikernels”, while offering compatibility with the Container Runtime Interface
(CRI). Unikernels are packaged inside OCI-compatible images and `urunc` launces
the unikernel on top of the underlying Virtual Machine or seccomp monitors.
Thus, developers and administrators can package, deliver, deploy and manage
unikernels using familiar cloud-native practices.

For the above purpose `urunc` acts as any other OCI runtime. The main
difference of `urunc` with other container runtimes is that instead of spawning
a simple process, it uses a Virtual Machine Monitor (VMM) or a Sandbox Monitor
to run the unikernel. It is important to note that `urunc` does not require any
particular software running alongside the user's application, inside or outside
the unikernel. As a result, `urunc` manages the user's application running
inside the unikernel through the respective VM process.

![demo](docs/img/urunc-nerdctl-example.gif)

## Quick start

The easiest and fastest way to try out `urunc` would be with `docker`
Before doing so, please make sure that the host system satisfies the
following dependencies:

- [docker](https://docs.docker.com/engine/install/ubuntu/)
- [Qemu](https://www.qemu.org/)
- `urunc` and `containerd-shim-urunc-v2` binaries.

Install Docker:

```bash
$ curl -fsSL https://get.docker.com -o get-docker.sh
$ sudo sh get-docker.sh
$ rm get-docker.sh
$ sudo groupadd docker
$ sudo usermod -aG docker $USER
```

Install `urunc`:

```bash
$ sudo apt-get install -y git
$ git clone https://github.com/urunc-dev/urunc.git
$ docker run --rm -ti -v $PWD/urunc:/urunc -w /urunc golang:latest bash -c "git config --global --add safe.directory /urunc && make"
$ sudo make -C urunc install
```

Install QEMU:

```bash
$ sudo apt install -y qemu-kvm
```

Now we are ready to run nginx as a Unikraft unikernel using Docker and `urunc`:

```bash
$ docker run --rm -d --runtime io.containerd.urunc.v2 harbor.nbfc.io/nubificus/urunc/nginx-qemu-unikraft:latest unikernel
67bec5ab9a748e35faf7c2079002177b9bdc806220e59b6b413836db1d6e4018
```

We can inspect the container and get its IP address:

```bash
$ docker inspect 67bec5ab9a748e35faf7c2079002177b9bdc806220e59b6b413836db1d6e4018 | grep IPAddress
            "SecondaryIPAddresses": null,
            "IPAddress": "172.17.0.2",
                    "IPAddress": "172.17.0.2",
```

At last we can curl the Nginx server running inside Unikraft with:

```bash
$ curl 172.17.0.2
<!DOCTYPE html>
<html>
<head>
  <title>Hello, world!</title>
</head>
<body>
  <h1>Hello, world!</h1>
  <p>Powered by <a href="http://unikraft.org">Unikraft</a>.</p>
</body>
</html>
```
## Installation guide

For a detailed installation guide please check [Documentation](#Documentation)
and particularly the [installation guide
page](https://urunc.io/installation/).

## Documentation

We keep an up to date documentation for `urunc` at 
https://urunc.io/ . We use [mkdocs](https://www.mkdocs.org/) to
render `urunc`'s documentation. Hence, you can also have a local preview of
documentation by running either `make docs` or `make docs_container`.

In the first case, `make docs` will execute `mkdocs serve`. Take a note of the
url, where the docs will be served in the output of the command
(i.e. http://127.0.0.1:8000). It is important to note, that `material-mkdocs`
must be installed. For more information, please check the [installation
guide](https://squidfunk.github.io/mkdocs-material/getting-started/).
Moreover, the pip packages `mkdocs-literate-nav` and `mkdocs-section-index`
should also be installed.

In the second case, a container with all dependencies will start serving
the documentation at http://127.0.0.1:8000.

## Supported platforms

At the moment, `urunc` is available on GNU/Linux for x86\_64 and arm64 architectures.
In addition, the following table provides an overview of the currently
supported VM/Sandbox monitors and unikernels:

| Unikernel  | VM/Sandbox Monitor         | Arch         | Storage                       |
|----------- |--------------------------- |------------- |------------------------------ |
| Rumprun    | Solo5-hvt, Solo5-spt       | x86,aarch64  | Block/Devmapper               |
| Unikraft   | QEMU, Firecracker          | x86          | Initrd, 9pfs                  |
| MirageOS   | QEMU, Solo5-hvt, Solo5-spt | x86,aarch64  | Block/Devmapper               |
| Mewz       | QEMU                       | x86          | In-memory                     |
| Linux      | QEMU, Firecracker          | x86          | Initrd, Block/Devmapper, 9pfs |

We plan to add support for more unikernel frameworks and other platforms too.
Feel free to [contact](#Contact) us for a specific unikernel framework or similar
technologies that you would like to see in `urunc`.

## Running on k8s

To use `urunc` with an existing Kubernetes cluster, please follow the
[instructions in the
docs](https://urunc.io/tutorials/How-to-urunc-on-k8s/).

## Publications and talks

A part of our work in `urunc` has been published in EuroSys'24 SESAME workshop,
under the title [Sandboxing Functions for Efficient and Secure Multi-tenant
Serverless Deployments](https://dl.acm.org/doi/10.1145/3642977.3652096). Feel
free to ask us if you can not have access to the paper.

Furthermore, `urunc` has appeared in various open source summits and events,
such as:

- [Open Source Summit
  2023](https://osseu2023.sched.com/event/1OGgY/urunc-a-unikernel-container-runtime-georgios-ntoutsos-anastassios-nanos-nubificus-ltd)
- [FOSDEM 2024 -- Containers devroom](https://archive.fosdem.org/2024/schedule/event/fosdem-2024-3402-from-containers-to-unikernels-navigating-integration-challenges-in-cloud-native-environments/)
- [KubeCon 2024](https://kccnceu2024.sched.com/event/1YeRd/unikernels-in-k8s-performance-and-isolation-for-serverless-computing-with-knative-anastassios-nanos-ioannis-plakas-nubis-pc)
- [FOSDEM 2025 -- Containers devroom](https://fosdem.org/2025/schedule/event/fosdem-2025-6284-less-overhead-strong-isolation-running-containers-in-minimal-specialized-linux-vms/)
- [FOSDEM 2025 -- WASM devroom](https://fosdem.org/2025/schedule/event/fosdem-2025-6292-wasm-meets-unikernels-secure-and-efficient-cloud-native-deployments/)

## Bug reporting

Please use [github issues](https://github.com/urunc-dev/urunc/issues) to report
any bug. For more info please check the [reporting
bugs](https://urunc.io/developer-guide/contribute/#reporting-bugs) section in
the [Contributing](https://urunc.io/developer-guide/contribute) guide.

## Roadmap

We track planned features, improvements, and milestones using [GitHub
Issues](https://github.com/urunc-dev/urunc/issues).  You can view the
[Roadmap](https://github.com/orgs/urunc-dev/projects/2) for each one of the
next (or previous) releases by filtering based on the respective milestone.

Have a feature in mind or want to contribute? Open an issue or fire-up a [discussion](https://github.com/urunc-dev/urunc/discussions)!

## Community and Meetings

Join us for our monthly open meetings, held every last Wednesday of the month.
These sessions are a great opportunity to share ideas, ask questions, and stay
connected with the project team and other contributors.

- Meeting Frequency: Monthly (last Wednesday of the month)
- Time: 15:00 UTC
- Format: Open agenda + roadmap review [Minutes & Agenda](https://docs.google.com/document/d/1hyFtbIqN__O4epiot-avn5LPDXwOsAX_qAQc2cjhgTE)
- Platform: [LF meetings](https://zoom-lfx.platform.linuxfoundation.org/meeting/91431746302?password=92a8b698-8d9d-43d3-9037-450804362208)
- Invitation: [link](https://zoom-lfx.platform.linuxfoundation.org/meeting/91431746302?password=92a8b698-8d9d-43d3-9037-450804362208&invite=true)
- [Slack channel](https://cloud-native.slack.com/archives/C08V201G35J)

## Contributing

We will be very happy to receive any feedback and any kind of contributions for
`urunc`. For more details please take a look in [`urunc`'s contributing
document](https://urunc.io/developer-guide/contribute/).

## Security policy

In case of a security vulnerability, please refer to the
[Security Policy document](https://urunc.io/developer-guide/security/).
for guidelines on how to report it responsibly.

## Changelog

See [CHANGELOG.md](https://github.com/urunc-dev/urunc/blob/main/CHANGELOG.md)
for more information on what changed in the latest and previous releases.

## License

[Apache License 2.0](LICENSE)

## Contact

We kindly invite everyone interested in `urunc` to join our
[Slack channel](https://cloud-native.slack.com/archives/C08V201G35J).
To directly communicate with the maintainers, feel free to drop an email At
[`urunc`'s maintainers' mailing list](cncf-urunc-maintainers@lists.cncf.io )
