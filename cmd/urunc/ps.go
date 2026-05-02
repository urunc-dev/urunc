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
	"encoding/json"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

var psCommand = &cli.Command{
	Name:      "ps",
	Usage:     "displays the host-visible monitor processes associated with a container",
	ArgsUsage: `<container-id>`,
	Description: `The ps command displays the host-visible process IDs associated
with a urunc container. This currently returns the host-visible monitor PID
stored in urunc state.json.

This command intentionally implements the runc-compatible interface required by
containerd-shim-runc-v2/go-runc:

    urunc ps --format json <container-id>

The JSON format must be a JSON array of integers, for example:

    [12345]
`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "format",
			Aliases: []string{"f"},
			Value:   "table",
			Usage:   "select output format: table or json",
		},
	},
	Action: func(_ context.Context, cmd *cli.Command) error {
		logrus.WithField("command", "PS").WithField("args", os.Args).Debug("urunc INVOKED")

		if err := checkArgs(cmd, 1, minArgs); err != nil {
			return err
		}

		unikontainer, err := getUnikontainer(cmd)
		if err != nil {
			return err
		}

		// The host-visible process for the current implementation is the
		// monitor process saved in state.json as State.Pid.
		//
		// Keep the return value as []int to match runc's ps implementation
		// and containerd/go-runc's expectation for `ps --format json`.
		pids := []int{unikontainer.State.Pid}

		switch cmd.String("format") {
		case "json":
			return json.NewEncoder(os.Stdout).Encode(pids)

		case "table":
			fmt.Fprintln(os.Stdout, "PID")
			for _, pid := range pids {
				fmt.Fprintln(os.Stdout, pid)
			}
			return nil

		default:
			return fmt.Errorf("invalid format option: %s", cmd.String("format"))
		}
	},
}
