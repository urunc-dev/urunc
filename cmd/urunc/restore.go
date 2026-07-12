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
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
	"golang.org/x/sys/unix"
)

var restoreCommand = &cli.Command{
	Name:  "restore",
	Usage: "restore a container from a previous checkpoint",
	ArgsUsage: `<container-id>

Where "<container-id>" is the name for the instance of the container to be
restored.`,
	Description: `The restore command creates a new container instance from the bundle and,
instead of cold-booting the guest, resumes the microVM from the snapshot
found in the image directory (see "urunc checkpoint").

The restored guest keeps the network identity (MAC, IP) it had at checkpoint
time, so the target network namespace must provide an equivalent L3
environment. The container always runs detached; the --detach flag is
accepted for CLI compatibility with runc.`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "image-path",
			Value: "",
			Usage: "path to criu image files for restoring",
		},
		&cli.StringFlag{
			Name:  "work-path",
			Value: "",
			Usage: "path for saving work files and logs (ignored, runc compatibility)",
		},
		&cli.StringFlag{
			Name:    "bundle",
			Aliases: []string{"b"},
			Value:   "",
			Usage:   "path to the root of the bundle directory, defaults to the current directory",
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
		&cli.BoolFlag{
			Name:    "detach",
			Aliases: []string{"d"},
			Usage:   "detach from the container's process (urunc always detaches)",
		},
		&cli.BoolFlag{
			Name:  "no-subreaper",
			Usage: "disable the use of the subreaper (ignored, runc compatibility)",
		},
		&cli.BoolFlag{
			Name:  "no-pivot",
			Usage: "do not use pivot root (ignored, runc compatibility)",
		},
		&cli.BoolFlag{
			Name:  "tcp-established",
			Usage: "allow open tcp connections (ignored, runc compatibility)",
		},
		&cli.BoolFlag{
			Name:  "ext-unix-sk",
			Usage: "allow external unix sockets (ignored, runc compatibility)",
		},
		&cli.BoolFlag{
			Name:  "shell-job",
			Usage: "allow shell jobs (ignored, runc compatibility)",
		},
		&cli.BoolFlag{
			Name:  "file-locks",
			Usage: "handle file locks, for safety (ignored, runc compatibility)",
		},
		&cli.BoolFlag{
			Name:  "lazy-pages",
			Usage: "use lazy migration mechanism (ignored, runc compatibility)",
		},
		&cli.StringFlag{
			Name:  "manage-cgroups-mode",
			Value: "",
			Usage: "cgroups mode: 'soft', 'full' and 'strict' (ignored, runc compatibility)",
		},
		&cli.StringSliceFlag{
			Name:  "empty-ns",
			Usage: "create a namespace, but don't restore its properties (ignored, runc compatibility)",
		},
		&cli.BoolFlag{
			Name:  "auto-dedup",
			Usage: "enable auto deduplication of memory images (ignored, runc compatibility)",
		},
	},
	Action: func(_ context.Context, cmd *cli.Command) error {
		logrus.WithField("command", "RESTORE").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, exactArgs); err != nil {
			return err
		}
		return restoreUnikontainer(cmd)
	},
}

// restoreUnikontainer creates the container with the restore annotation set,
// starts it (the reexec process launches the VMM in restore mode instead of
// cold-booting) and then completes the restore through the VMM API.
func restoreUnikontainer(cmd *cli.Command) error {
	imagePath, err := resolveImagePath(cmd)
	if err != nil {
		return err
	}
	if _, err := os.Stat(imagePath); err != nil {
		return fmt.Errorf("cannot access checkpoint image path %s: %w", imagePath, err)
	}

	uruncCfg, _ := unikontainers.LoadUruncConfig(unikontainers.UruncConfigPath) // ignore the error and use default config
	if err := createUnikontainer(cmd, uruncCfg, imagePath); err != nil {
		return err
	}

	if err := startUnikontainer(cmd); err != nil {
		return err
	}

	// The VMM process is up; drive its API to load/resume the snapshot.
	unikontainer, err := getUnikontainer(cmd)
	if err != nil {
		return err
	}
	if err := unikontainer.FinishRestore(); err != nil {
		// The freshly spawned VMM cannot recover from a failed
		// restore; stop it so the container does not linger half-dead.
		if killErr := unikontainer.Signal(unix.SIGKILL); killErr != nil {
			logrus.WithError(killErr).Error("failed to kill VMM after failed restore")
		}
		return err
	}

	return nil
}
