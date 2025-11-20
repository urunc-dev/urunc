// Copyright (c) 2023-2025, Nubificus LTD
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
	m "github.com/urunc-dev/urunc/internal/metrics"
)

var startCommand = &cli.Command{
	Name:  "start",
	Usage: "executes the user defined process in a created container",
	ArgsUsage: `<container-id>

Where "<container-id>" is your name for the instance of the container that you
are starting. The name you provide for the container instance must be unique on
your host.`,
	Description: `The start command executes the user defined process in a created container.`,
	Action: func(_ context.Context, cmd *cli.Command) error {
		logrus.WithField("command", "START").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, exactArgs); err != nil {
			return err
		}
		return startUnikontainer(cmd)
	},
}

// We keep it as a separate function, since it is also called from
// the run command
func startUnikontainer(cmd *cli.Command) error {
	// No need to check if containerID is valid, because it will get
	// checked later. We just want it for the metrics
	containerID := cmd.Args().First()
	metrics.Capture(containerID, m.TS11)

	// get Unikontainer data from state.json
	unikontainer, err := getUnikontainer(cmd)
	if err != nil {
		return err
	}
	metrics.Capture(containerID, m.TS12)

	sockAddr := unikontainers.GetStartSockAddr(unikontainer.BaseDir)
	listener, cleaner, err := unikontainers.CreateListener(sockAddr, true)
	if err != nil {
		return err
	}
	defer cleaner()

	err = unikontainer.SendStartExecve()
	if err != nil {
		return err
	}
	metrics.Capture(containerID, m.TS13)

	// wait ContainerStarted message on start.sock from reexec process
	err = unikontainers.AwaitMessage(listener, unikontainers.ContainerStarted)
	if err != nil {
		return err
	}

	err = unikontainer.SetRunningState()
	if err != nil {
		return err
	}

	return unikontainer.ExecuteHooks("Poststart")
}
