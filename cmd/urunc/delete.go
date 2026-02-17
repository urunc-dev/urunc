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
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

var deleteCommand = &cli.Command{
	Name:  "delete",
	Usage: "delete any resources held by the container often used with detached container",
	ArgsUsage: `<container-id>

Where "<container-id>" is the name for the instance of the container.

EXAMPLE:
For example, if the container id is "ubuntu01" and urunc list currently shows the
status of "ubuntu01" as "stopped" the following will delete resources held for
"ubuntu01" removing "ubuntu01" from the urunc list of containers:

	# urunc delete ubuntu01`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "force",
			Aliases: []string{"f"},
			Usage:   "Forcibly deletes the container if it is still running (uses SIGKILL)",
		},
	},
	Action: func(_ context.Context, cmd *cli.Command) error {
		runtime.GOMAXPROCS(1)
		runtime.LockOSThread()
		logrus.WithField("command", "DELETE").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, exactArgs); err != nil {
			return err
		}

		// get Unikontainer data from state.json
		unikontainer, err := getUnikontainer(cmd)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				containerID := cmd.Args().First()
				if containerID == "" {
					return ErrEmptyContainerID
				}
				rootDir := cmd.String("root")
				containerDir := filepath.Join(rootDir, containerID)
				e := os.RemoveAll(containerDir)
				if e != nil {
					logrus.Errorf("remove %s: %v", containerDir, e)
				}
				if cmd.Bool("force") {
					return nil
				}
			}
			return err
		}
		if cmd.Bool("force") {
			err := unikontainer.Kill()
			if err != nil {
				return err
			}
		}
		err = unikontainer.Delete()
		if err != nil {
			return err
		}

		return unikontainer.ExecuteHooks("Poststop")
	},
}
