// Copyright (c) 2023-2026, Nubificus LTD
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package constants

const TimestampTargetFile = "/tmp/urunc.zlog"

// ContainerRootfsMountPath is the path inside the monitor rootfs with the
// container's image rootfs and the boot files.
const ContainerRootfsMountPath = "/cntrRootfs"

// MonitorRootfsDirName is the name of the directory for the monitor rootfs
const MonitorRootfsDirName = "monRootfs"

// VAccelMountPath is the directory inside the monitor rootfs with the vAccel
// unix sockets: the agent's socket from the host and the monitor's own one.
const VAccelMountPath = "/vaccel"

// ContainerBootDir is the directory inside the monitor rootfs that holds the
// boot files of a generic container boot: the host kernel and boot initrd are
// bind-mounted read-only here, and the per-container initrd is written next to
// them. It is a sibling of ContainerRootfsMountPath and never shared with the
// guest, so the boot files do not leak into the container's rootfs.
const ContainerBootDir = "/urunc-boot"

// ContainerBootKernelPath is the monitor rootfs path of the host kernel
// selected with the bootKernel annotation.
const ContainerBootKernelPath = ContainerBootDir + "/kernel"

// ContainerBootInitrdPath is the monitor rootfs path of the host boot initrd
// selected with the bootInitrd annotation. It is mounted read-only, since the
// same file is shared by every container booting from it.
const ContainerBootInitrdPath = ContainerBootDir + "/initrd"

// ContainerBootGuestInitrdPath is the initrd the guest actually boots: a
// private copy of ContainerBootInitrdPath with the urunit configuration of this
// container appended to it.
const ContainerBootGuestInitrdPath = ContainerBootDir + "/initrd.urunc"

// AgentVsockUDSPath is the monitor-rootfs path of the vsock unix socket that
// firecracker creates for the in-guest exec agent. Unlike qemu, which reaches a
// guest's vsock through the host's /dev/vhost-vsock, firecracker multiplexes
// vsock over this host unix socket: urunc exec connects to it and asks for the
// agent's guest port with a "CONNECT <port>" line. It is relative to the
// pivoted monitor rootfs, so the host path is <monRootfs>/urunc-agent.vsock.
const AgentVsockUDSPath = "/urunc-agent.vsock"
