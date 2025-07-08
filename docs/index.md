# urunc: A Lightweight Container Runtime for Unikernels

The main goal of `urunc` is to bridge the gap between traditional unikernels and
containerized environments, enabling seamless integration with cloud-native
architectures. Designed to fully leverage the container semantics and benefits
from the OCI tools and methodology, `urunc` aims to become
“runc for unikernels”, while offering compatibility with the Container
Runtime Interface (CRI). Unikernels are packaged inside OCI-compatible images
and `urunc` launches the unikernel on top of the underlying Virtual Machine or
seccomp monitors. Thus, developers and administrators can package, deliver,
deploy and manage unikernels using familiar cloud-native practises.

For the above purpose `urunc` acts as any other OCI runtime. The main
difference of `urunc` with other container runtimes is that instead of
spawning a simple process, it uses a Virtual Machine Monitor (VMM) or a sandbox
monitor to run the unikernel. It is important to note that `urunc` does not
require any particular software running alongise the user's application inside
or outside the unikernel. As a result, `urunc` is able to support any unikernel
framework or similar technologies, while maintaining as low overhead as
possible.

## Key features

- OCI Compatibility: Compatible with the Open Container Initiative (OCI) standards, enabling the use of existing container tools and workflows.
- Container Runtime Interface (CRI) Support: Compatible with Kubernetes and other CRI-based systems for seamless integration into container orchestration platforms.
- Unikernel Support: Run applications and user code as unikernels, unlocking the performance and security advantages of unikernel technology.
- Integration with VMMs and other strong sandboxing mechanisms: Use lightweight VMMs or sandbox monitors to launch unikernels, facilitating efficient resource isolation and management.
- Un-opinionated and Extensible: Straightforward and easy integration of new unikernel frameworks and sandboxing mechanisms without any porting overhead.

## Use cases

Unikernels are well known as a good fit for a variety of use cases, such as:

- Microservices: The lightweight and almost deminished *OS noise* of unikernels
  can significantly improve the execution of applications, making unikernels an
  attractive fit for microservices.
- Serverless and FaaS: The extremely fast instantiation time of unikernels
  satisfies the event-driven, short-lived and scalable characteristics of
  serverless computing
- Edge computing: The lightweight notion of unikernels suits very well with edge
  devices, where resources constraints and performance are critical.
- Sensitive environments: The inherited strong VM-based isolation, along with
  the minimized attack surface of unikernels, provide strong security guarantees
  for sensitive applications which demand high security standards.

In all the above use cases, `urunc` facilitates the seamless integration of
unikernels with existing cloud-native tools and technologies, enabling the effortless
distribution and management of applications running as unikernels.

## Current support of unikernels and VM/Sandbox monitors

The following table provides an overview of the currently supported VMMs and
Sandbox monitors, along with the unikernels that can run on top of them.


| Unikernel                               | VM/Sandbox Monitor   | Arch         | Storage    |
|---------------------------------------- |--------------------- |------------- |----------- |
| [Rumprun](./unikernel-support#rumprun)  | [Solo5-hvt](./hypervisor-support#solo5-hvt), [Solo5-spt](./hypervisor-support#solo5-spt) | x86, aarch64  | Block/Devmapper  |
| [Unikraft](./unikernel-support#unikraft)| [Qemu](./hypervisor-support#qemu), [Firecracker](./hypervisor-support#aws-firecracker) | x86          | Initrd, 9pfs |
| [MirageOS](./unikernel-support#mirage)| [Qemu](./hypervisor-support#qemu), [Solo5-hvt](./hypervisor-support#solo5-hvt), [Solo5-spt](./hypervisor-support#solo5-spt) | x86, aarch64          | Block/Devmapper |
| [Mewz](./unikernel-support#mewz)| [Qemu](./hypervisor-support#qemu) | x86 | In-memory |
| [Linux](./unikernel-support#linux)| [Qemu](./hypervisor-support#qemu), [Firecracker](./hypervisor-support#aws-firecracker) | x86, aarch64 | Initrd, Block/Devmapper, 9pfs |

<!-- ## urunc and the CNCF -->

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

## Quick links

- [Urunc Slack channel](https://cloud-native.slack.com/archives/C08V201G35J)
- [Contributing](developer-guide/contribute/)
- [Getting metrics from `urunc`](developer-guide/timestamps)
- [Integration with k8s](tutorials/How-to-urunc-on-k8s/)

<hr>

<p align="center">
urunc is a <a href="https://cncf.io">Cloud Native Computing Foundation</a> sandbox project.
</p>

<p align="center">
<img src="assets/images/cncf-color.svg#only-light" width="500px"/> 
<img src="assets/images/cncf-white.svg#only-dark" width="500px"/>
</p>
