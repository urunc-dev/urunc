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
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/opencontainers/runc/libcontainer"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	m "github.com/urunc-dev/urunc/internal/metrics"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
)

// monitorArgv is the internal argv of the urunc monitor process.
const monitorArgv = "monitor"

// libcontainerCreate is "urunc create" when the libcontainer runtime is
// enabled. It configures libcontainer to spawn a specific urunc process
// ("urunc monitor") which will finalize the execution environment for a
// monitor and then execve it.
func libcontainerCreate(cmd *cli.Command, uruncCfg *unikontainers.UruncConfig) (err error) {
	unikontainer, err := newUnikontainer(cmd, uruncCfg)
	if err != nil {
		return err
	}

	config, err := unikontainer.BuildContainerConfig(cmd.Bool("systemd-cgroup"))
	if err != nil {
		return err
	}

	container, err := libcontainer.Create(unikontainer.LibcontainerRoot(), unikontainer.State.ID, config)
	if err != nil {
		return fmt.Errorf("failed to create the monitor's container: %w", err)
	}
	defer func() {
		// Best effort cleanup in case of failure. Any cgroups or libcontainer
		// metadata residuals can cause issues with later container creates.
		if err != nil {
			tmpErr := container.Destroy()
			if tmpErr != nil {
				logrus.WithError(tmpErr).Error("failed to destroy the monitor's container")
			}
		}
	}()

	// The write end of the ready pipe. The monitor process inherits the
	// write end of the ready pipe and reports the outcome of its setup
	// back to "urunc start" over it.

	readyFile, err := unikontainers.CreateReadyPipe(unikontainer.BaseDir)
	if err != nil {
		return err
	}
	defer readyFile.Close()

	process, err := monitorProcess(cmd, unikontainer, readyFile)
	if err != nil {
		return err
	}
	metrics.Capture(m.TS03)

	err = container.Start(process)
	if err != nil {
		return fmt.Errorf("failed to start the monitor's init process: %w", err)
	}
	// The init process is now waiting the exec fifo. If anything below fails,
	// terminate and reap it before the Destroy above tears down the cgroup, so the
	// reap does not race the cgroup removal (same order as runc).
	defer func() {
		if err != nil {
			_ = process.Signal(os.Kill)
			_, _ = process.Wait()
		}
	}()

	state, err := container.State()
	if err != nil {
		return fmt.Errorf("failed to read the monitor's container state: %w", err)
	}
	metrics.Capture(m.TS06)

	err = unikontainer.Create(state.InitProcessPid, cmd.String("pid-file"))
	if err != nil {
		return err
	}
	metrics.Capture(m.TS08)

	return nil
}

// buildMonitorArgs assembles the "urunc monitor" argv setting the same log format
// and enabling debug logs based on the current urunc process.
// TODO: THe logs will be printed in stdout. We need to pass another fd with the logs
// in order to store them in the correct file.
func buildMonitorArgs(id string, logFormat string) []string {
	args := []string{"/proc/self/exe"}
	logLevel := logrus.GetLevel()
	if logLevel >= logrus.DebugLevel {
		// TODO: We need to pass the log level not just debug.
		// However, this needs to change the cli args of urunc.
		// So let's do it in future iteration.
		args = append(args, "--debug")
	}

	if logFormat != "" {
		args = append(args, "--log-format", logFormat)
	}

	args = append(args, monitorArgv, id)

	return args
}

// monitorProcess describes the urunc process that libcontainer starts.
// Args[0] is "/proc/self/exe" because libcontainer resolves it after the
// pivot: /proc is always mounted in the monitor rootfs, so this re-execs the
// sealed urunc binary without urunc having to be copied into that rootfs.
func monitorProcess(cmd *cli.Command, u *unikontainers.Unikontainer, readyFile *os.File) (*libcontainer.Process, error) {
	process := &libcontainer.Process{
		Args: buildMonitorArgs(u.State.ID, cmd.String("log-format")),
		Env:  os.Environ(),
		// "/" rather than the spec's Cwd: that one is the guest's working
		// directory
		Cwd: "/",
		// TODO set uid, gid and additional groups here
		Init:         true,
		Capabilities: unikontainers.MonitorCapabilities(u.Spec),
		LogLevel:     strconv.Itoa(int(logrus.GetLevel())),
		// The ready pipe's write end. It must be the only/first ExtraFile so the
		// monitor inherits it at unikontainers.ReadyPipeFD:
		ExtraFiles: []*os.File{readyFile},
	}

	if !u.Spec.Process.Terminal {
		process.Stdin = os.Stdin
		process.Stdout = os.Stdout
		process.Stderr = os.Stderr

		return process, nil
	}

	// With a terminal, libcontainer's init allocates the pty and passes
	// the master end over the console socket itself The console socket is
	// an AF_UNIX socket the caller (e.g. containerd) is listening on, so
	// it must be dialed, not opened as a file: open(2) on a socket inode
	// fails with ENXIO.
	consoleSocket := cmd.String("console-socket")
	if consoleSocket == "" {
		return nil, fmt.Errorf("the container requests a terminal but no console socket was given")
	}
	conn, err := net.Dial("unix", consoleSocket)
	if err != nil {
		return nil, fmt.Errorf("failed to dial the console socket %s: %w", consoleSocket, err)
	}
	defer conn.Close()

	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, fmt.Errorf("console socket connection is not a unix socket")
	}
	// File returns a dup of the socket fd, independent of conn, so closing conn
	// above does not affect it. libcontainer passes it to its init as an extra
	// file and sends the pty master over it.
	socket, err := unixConn.File()
	if err != nil {
		return nil, fmt.Errorf("failed to get the console socket file: %w", err)
	}
	process.ConsoleSocket = socket

	return process, nil
}
