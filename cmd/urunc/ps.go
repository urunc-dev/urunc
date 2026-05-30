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
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

// getAllDescendants returns the given pid and all its descendant PIDs
// by walking /proc/<pid>/task/<pid>/children recursively.
func getAllDescendants(rootPid int) []int {
	var pids []int
	queue := []int{rootPid}

	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		pids = append(pids, pid)

		childrenPath := fmt.Sprintf("/proc/%d/task/%d/children", pid, pid)
		data, err := os.ReadFile(childrenPath)
		if err != nil {
			// process may have exited, skip silently
			continue
		}

		for _, field := range strings.Fields(string(data)) {
			childPid, err := strconv.Atoi(field)
			if err == nil {
				queue = append(queue, childPid)
			}
		}
	}

	return pids
}

var psCommand = &cli.Command{
	Name:      "ps",
	Usage:     "displays the host-visible monitor processes associated with a container",
	ArgsUsage: `<container-id>`,
	Description: `The ps command displays the host-visible process IDs associated
with a urunc container. It returns all host-visible PIDs including the monitor
process and all its descendants (e.g. VMM sub-processes, virtiofsd).

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

		// Return all host-visible PIDs (monitor + descendants) to match runc's
		// ps implementation and containerd/go-runc's expectation for `ps --format json`.
		pids := getAllDescendants(unikontainer.State.Pid)

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
