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
