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

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

var errExecNotSupported = errors.New("exec is not supported for urunc guests")

// execCommand handles exec for containers that urunc handed over to runc.
// The flags mirror the ones of runc exec, so that the arguments containerd
// passes are accepted. They are not used by urunc, since runc gets the
// original arguments.
var execCommand = &cli.Command{
	Name:      "exec",
	Usage:     "execute new process inside a container handed over to runc",
	ArgsUsage: `<container-id> <command> [command options]  || -p process.json <container-id>`,
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "console-socket"},
		&cli.StringFlag{Name: "pidfd-socket"},
		&cli.StringFlag{Name: "cwd"},
		&cli.StringSliceFlag{Name: "env", Aliases: []string{"e"}},
		&cli.BoolFlag{Name: "tty", Aliases: []string{"t"}},
		&cli.StringFlag{Name: "user", Aliases: []string{"u"}},
		&cli.StringSliceFlag{Name: "additional-gids", Aliases: []string{"g"}},
		&cli.StringFlag{Name: "process", Aliases: []string{"p"}},
		&cli.BoolFlag{Name: "detach", Aliases: []string{"d"}},
		&cli.StringFlag{Name: "pid-file"},
		&cli.StringFlag{Name: "process-label"},
		&cli.StringFlag{Name: "apparmor"},
		&cli.BoolFlag{Name: "no-new-privs"},
		&cli.StringSliceFlag{Name: "cap", Aliases: []string{"c"}},
		&cli.IntFlag{Name: "preserve-fds"},
		&cli.StringSliceFlag{Name: "cgroup"},
		&cli.BoolFlag{Name: "ignore-paused"},
	},
	Action: func(_ context.Context, cmd *cli.Command) error {
		logrus.WithField("command", "EXEC").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, minArgs); err != nil {
			return err
		}

		// For a container that urunc handed over to runc, this execs runc
		// with the original arguments and does not return.
		if _, err := getUnikontainer(cmd); err != nil {
			return err
		}

		return errExecNotSupported
	},
}
