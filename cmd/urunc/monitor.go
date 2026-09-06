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

package main

import (
	"context"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
)

// monitorCommand is not part of the OCI runtime interface and no user or shim
// ever calls it: it is the argv libcontainer's init execve's inside the
// monitor rootfs. By that point the vast majority of the execution environment
// (namespaces, cgroups, mounts, etc.) are all in place and all
// that is left is to create the tap device and become the monitor.
// It takes the container ID purely so that the logs and the process listing name
// the container; everything it actually needs comes from the monitor spec file that
// create wrote inside the monitor's rootfs.
var monitorCommand = &cli.Command{
	Name:      monitorArgv,
	Usage:     "internal: launch the monitor from inside its own rootfs",
	ArgsUsage: `<container-id>`,
	Hidden:    true,
	Action: func(_ context.Context, cmd *cli.Command) error {
		logrus.WithField("command", "MONITOR").WithField("args", os.Args).Debug("urunc INVOKED")
		err := checkArgs(cmd, 1, exactArgs)
		if err != nil {
			return err
		}
		metrics.SetLoggerContainerID(cmd.Args().First())

		// ExecMonitor does not return on success: the monitor replaces this
		// process and becomes the container process.
		return unikontainers.ExecMonitor(metrics)
	},
}

// isMonitorInvocation reports whether this process is the "urunc monitor" that
// libcontainer starts inside the monitor container.
func isMonitorInvocation(cmd *cli.Command) bool {
	return cmd.Args().First() == monitorArgv
}
