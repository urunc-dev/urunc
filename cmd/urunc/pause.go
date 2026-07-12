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
)

var pauseCommand = &cli.Command{
	Name:  "pause",
	Usage: "pause suspends all processes inside the container",
	ArgsUsage: `<container-id>

Where "<container-id>" is the name for the instance of the container to be
paused. `,
	Description: `The pause command suspends the container by pausing the vCPUs of its
microVM through the VMM control API. Use "urunc resume" to resume it.`,
	Action: func(_ context.Context, cmd *cli.Command) error {
		logrus.WithField("command", "PAUSE").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, exactArgs); err != nil {
			return err
		}
		unikontainer, err := getUnikontainer(cmd)
		if err != nil {
			return err
		}
		return unikontainer.PauseVM()
	},
}

var resumeCommand = &cli.Command{
	Name:  "resume",
	Usage: "resumes all processes that have been previously paused",
	ArgsUsage: `<container-id>

Where "<container-id>" is the name for the instance of the container to be
resumed.`,
	Description: `The resume command resumes the vCPUs of the container's paused microVM
through the VMM control API.`,
	Action: func(_ context.Context, cmd *cli.Command) error {
		logrus.WithField("command", "RESUME").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, exactArgs); err != nil {
			return err
		}
		unikontainer, err := getUnikontainer(cmd)
		if err != nil {
			return err
		}
		return unikontainer.ResumeVM()
	},
}
