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
	"runtime"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

var killCommand = &cli.Command{
	Name:  "kill",
	Usage: "kill sends the specified signal (default: SIGTERM) to the container's init process",
	ArgsUsage: `<container-id> [signal]

Where "<container-id>" is the name for the instance of the container and
"[signal]" is the signal to be sent to the init process.

EXAMPLE:
For example, if the container id is "ubuntu01" the following will send a "KILL"
signal to the init process of the "ubuntu01" container:

	# urunc kill ubuntu01 KILL`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "all",
			Aliases: []string{"a"},
			Usage:   "send the specified signal to all processes inside the container",
		},
	},
	Action: func(_ context.Context, cmd *cli.Command) error {
		runtime.GOMAXPROCS(1)
		runtime.LockOSThread()
		logrus.WithField("command", "KILL").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, minArgs); err != nil {
			return err
		}
		if err := checkArgs(cmd, 2, maxArgs); err != nil {
			return err
		}

		// get Unikontainer data from state.json
		unikontainer, err := getUnikontainer(cmd)
		if err != nil {
			return err
		}
		return unikontainer.Kill()
	},
}
