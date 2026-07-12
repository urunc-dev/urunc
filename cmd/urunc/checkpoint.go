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
	"path/filepath"
	"runtime"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

var checkpointCommand = &cli.Command{
	Name:  "checkpoint",
	Usage: "checkpoint a running container",
	ArgsUsage: `<container-id>

Where "<container-id>" is the name for the instance of the container to be
checkpointed.`,
	Description: `The checkpoint command pauses the container's microVM and saves a full
snapshot of its state (device model and guest memory) into the image
directory, so it can later be restored with "urunc restore".

Unlike runc, urunc does not use CRIU: the snapshot is taken by the VMM
(Firecracker or Cloud Hypervisor) through its control API. CRIU-specific
options are accepted for CLI compatibility with runc, but are ignored.`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "image-path",
			Value: "",
			Usage: "path for saving checkpoint image files",
		},
		&cli.StringFlag{
			Name:  "work-path",
			Value: "",
			Usage: "path for saving work files and logs (ignored, runc compatibility)",
		},
		&cli.StringFlag{
			Name:  "parent-path",
			Value: "",
			Usage: "path for previous criu image files in pre-dump (ignored, runc compatibility)",
		},
		&cli.BoolFlag{
			Name:  "leave-running",
			Usage: "leave the container running after writing the checkpoint to disk",
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
			Name:  "lazy-pages",
			Usage: "use lazy migration mechanism (ignored, runc compatibility)",
		},
		&cli.StringFlag{
			Name:  "status-fd",
			Value: "",
			Usage: "criu writes \\0 to this FD once lazy-pages is ready (ignored, runc compatibility)",
		},
		&cli.StringFlag{
			Name:  "page-server",
			Value: "",
			Usage: "ADDRESS:PORT of the page server (ignored, runc compatibility)",
		},
		&cli.BoolFlag{
			Name:  "file-locks",
			Usage: "handle file locks, for safety (ignored, runc compatibility)",
		},
		&cli.BoolFlag{
			Name:  "pre-dump",
			Usage: "dump container's memory information only, leave the container running after this (unsupported)",
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
		// Kill (the default post-checkpoint action) joins the sandbox
		// network namespace, which requires a locked OS thread.
		runtime.GOMAXPROCS(1)
		runtime.LockOSThread()
		logrus.WithField("command", "CHECKPOINT").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, exactArgs); err != nil {
			return err
		}

		unikontainer, err := getUnikontainer(cmd)
		if err != nil {
			return err
		}

		imagePath, err := resolveImagePath(cmd)
		if err != nil {
			return err
		}

		return unikontainer.Checkpoint(imagePath, cmd.Bool("leave-running"))
	},
}

// resolveImagePath returns the checkpoint image directory, defaulting to
// "checkpoint" under the current working directory like runc does.
func resolveImagePath(cmd *cli.Command) (string, error) {
	imagePath := cmd.String("image-path")
	if imagePath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		imagePath = filepath.Join(cwd, "checkpoint")
	}
	return filepath.Abs(imagePath)
}
