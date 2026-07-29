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
	"fmt"
	"os"

	"github.com/opencontainers/runc/libcontainer"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	m "github.com/urunc-dev/urunc/internal/metrics"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
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
	containerID := cmd.Args().First()
	metrics.SetLoggerContainerID(containerID)
	metrics.Capture(m.TS11)

	// get Unikontainer data from state.json
	unikontainer, err := getUnikontainer(cmd)
	if err != nil {
		return err
	}
	metrics.Capture(m.TS12)

	err = unikontainer.CreateListener(!unikontainers.FromReexec)
	if err != nil {
		return err
	}
	// NOTE: We ignore any errors from the DestroyListener here, because
	// the reexec process has already started the monitor execution and hence
	// returning an error would confuse the shim. Furthermore, this process
	// exits. However, we might want to revisit this in the future and
	// handle it better.
	defer func() {
		tmpErr := unikontainer.DestroyListener(!unikontainers.FromReexec)
		if tmpErr != nil {
			logrus.WithError(tmpErr).Error("failed to destroy the start listener")
		}
	}()

	// Start the monitor environment setup process. For libcontainer the heavy lifting
	// has be done from libcontainer and we just need to finalize the setup.
	// For urunc native way, we need to setup the monitor rootfs too.
	if unikontainer.UruncCfg.Runtime.Libcontainer {
		err = startMonitorInit(unikontainer)
	} else {
		err = startReexec(unikontainer)
	}
	if err != nil {
		return err
	}
	metrics.Capture(m.TS13)

	// Wait for the monitor process to report a successful start.
	err = unikontainer.AwaitMsg(unikontainers.StartSuccess)
	if err != nil {
		err = fmt.Errorf("failed to get the success message from the monitor: %w", err)
		return err
	}

	err = unikontainer.SetRunningState()
	if err != nil {
		err = fmt.Errorf("failed to set the state as running for container: %w", err)
		return err
	}

	return unikontainer.ExecuteHooks("Poststart")
}

// startMonitorInit starts the urunc monitor process, blocked since create from
// libcontainers, by opening the exec fifo.
func startMonitorInit(unikontainer *unikontainers.Unikontainer) error {
	container, err := libcontainer.Load(unikontainer.LibcontainerRoot(), unikontainer.State.ID)
	if err != nil {
		return fmt.Errorf("failed to load the monitor's container: %w", err)
	}

	err = container.Exec()
	if err != nil {
		return fmt.Errorf("failed to release the monitor process: %w", err)
	}

	return nil
}

// startReexec tells urunc's reexec process to set up the monitor's
// environment and launch it.
func startReexec(unikontainer *unikontainers.Unikontainer) error {
	err := unikontainer.CreateConn(!unikontainers.FromReexec)
	if err != nil {
		return fmt.Errorf("failed to create connection with reexec socket: %w", err)
	}
	sendErr := unikontainer.SendMessage(unikontainers.StartExecve)
	if sendErr != nil {
		logrus.WithError(sendErr).Error("failed to send START message to reexec")
		sendErr = fmt.Errorf("error sending START message: %w", sendErr)
	}
	// Regardless of the SendMessage status, make sure to clean up the socket,
	// since it is not required anymore
	cleanErr := unikontainer.DestroyConn(!unikontainers.FromReexec)
	if cleanErr != nil {
		logrus.WithError(cleanErr).Error("failed to destroy connection to reexec socket")
		cleanErr = fmt.Errorf("error destroying connection to reexec socket: %w", cleanErr)
	}

	return errors.Join(sendErr, cleanErr)
}
