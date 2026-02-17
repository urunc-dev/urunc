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

var runUsage = `<container-id>

Where "<container-id>" is your name for the instance of the container that you
are starting. The name you provide for the container instance must be unique on
your host.`

var runDescription = `The run command creates an instance of a container for a bundle. The bundle
is a directory with a specification file named "` + specConfig + `" and a root
filesystem.

The specification file includes an args parameter. The args parameter is used
to specify command(s) that get run when the container is started. To change the
command(s) that get executed on start, edit the args parameter of the spec. See
"urunc spec --help" for more explanation.`

var runCommand = &cli.Command{
	Name:        "run",
	Usage:       "create and run a container",
	ArgsUsage:   runUsage,
	Description: runDescription,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "bundle",
			Aliases: []string{"b"},
			Value:   "",
			Usage:   `path to the root of the bundle directory, defaults to the current directory`,
		},
		&cli.StringFlag{
			Name:  "console-socket",
			Value: "",
			Usage: "path to an AF_UNIX socket which will receive a file descriptor referencing the master end of the console's pseudoterminal",
		},
		&cli.StringFlag{
			Name:  "pid-file",
			Value: "",
			Usage: "specify the file to write the process id to",
		},
	},
	Action: func(_ context.Context, cmd *cli.Command) error {
		logrus.WithField("command", "RUN").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, exactArgs); err != nil {
			return err
		}

		// FIXME: This is a refactor of what the previous code did, however I have a feeling
		// that it will not work...
		if err := reexecUnikontainer(cmd); err != nil {
			return err
		}
		if err := startUnikontainer(cmd); err != nil {
			return err
		}
		return nil
	},
}
