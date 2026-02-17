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
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"

	"github.com/creack/pty"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	m "github.com/urunc-dev/urunc/internal/metrics"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
	"golang.org/x/sys/unix"
)

var createUsage = `<container-id>
Where "<container-id>" is your name for the instance of the container that you
are starting. The name you provide for the container instance must be unique on
your host.`
var createDescription = `
The create command creates an instance of a container for a bundle. The bundle
is a directory with a specification file named "` + specConfig + `" and a root
filesystem.`

var createCommand = &cli.Command{
	Name:        "create",
	Usage:       "create a container",
	ArgsUsage:   createUsage,
	Description: createDescription,
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
		&cli.BoolFlag{
			Name: "reexec",
		},
	},
	Action: func(_ context.Context, cmd *cli.Command) error {
		logrus.WithField("command", "CREATE").WithField("args", os.Args).Debug("urunc INVOKED")
		if err := checkArgs(cmd, 1, exactArgs); err != nil {
			return err
		}
		if !cmd.Bool("reexec") {
			uruncCfg, _ := unikontainers.LoadUruncConfig(unikontainers.UruncConfigPath) // ignore the error and use default config
			return createUnikontainer(cmd, uruncCfg)
		}

		return reexecUnikontainer(cmd)
	},
}

// createUnikontainer creates a Unikernel struct from bundle data,
// initializes it's base dir and state.json,
// setups terminal if required and spawns reexec process,
// waits for reexec process to notify, executes CreateRuntime hooks,
// sends ACK to reexec process
func createUnikontainer(cmd *cli.Command, uruncCfg *unikontainers.UruncConfig) (err error) {
	err = nil
	containerID := cmd.Args().First()
	if containerID == "" {
		err = fmt.Errorf("container id cannot be empty")
		return err
	}
	metrics.SetLoggerContainerID(containerID)
	metrics.Capture(m.TS00)

	// We have already made sure in main.go that root is not nil
	rootDir := cmd.String("root")

	// bundle option cli option is optional. Therefore the bundle directory
	// is either the CWD or the one defined in the cli option
	bundlePath := cmd.String("bundle")
	if bundlePath == "" {
		bundlePath, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	// new unikernel from bundle
	unikontainer, err := unikontainers.New(bundlePath, containerID, rootDir, uruncCfg)
	if err != nil {
		if errors.Is(err, unikontainers.ErrQueueProxy) ||
			errors.Is(err, unikontainers.ErrNotUnikernel) {
			// Exec runc to handle non unikernel containers
			err = runcExec()
			return err
		}
		return err
	}
	metrics.Capture(m.TS01)

	err = unikontainer.InitialSetup()
	if err != nil {
		return err
	}

	metrics.Capture(m.TS02)

	// Create socket for nsenter
	initSockParent, initSockChild, err := newSockPair("init")
	if err != nil {
		err = fmt.Errorf("failed to create init socket: %w", err)
		return err
	}
	defer func() {
		tmpErr := initSockParent.Close()
		if tmpErr != nil && err == nil {
			err = fmt.Errorf("failed to close parent socket pair: %w", tmpErr)
			return
		}
	}()

	// Create log pipe for nsenter
	// NOTE: We might want to switch form pipe to socketpair for logs too.
	logPipeParent, logPipeChild, err := os.Pipe()
	if err != nil {
		err = fmt.Errorf("failed to create pipe for logs: %w", err)
		return err
	}

	// get the data to send to nsenter
	nsenterInfo, err := unikontainer.FormatNsenterInfo()
	if err != nil {
		err = fmt.Errorf("failed to format namespace info for nsenter: %w", err)
		return err
	}

	// Setup reexecCommand
	reexecCommand := createReexecCmd(initSockChild, logPipeChild)

	// Create a go func to handle logs from nsenter
	logsDone := ForwardLogs(logPipeParent)

	// Start reexec process
	metrics.Capture(m.TS03)
	// setup terminal if required and start reexec process
	// TODO: This part of code needs better rhandling. It is not the
	// job of the urunc create to setup the terminal for reexec.
	// The main concern is the nsenter execution before the reexec.
	// If anything goes wrong and we mess up with nsenter debugging
	// is extremely hard.
	if unikontainer.Spec.Process.Terminal {
		ptm, err := pty.Start(reexecCommand)
		if err != nil {
			err = fmt.Errorf("failed to setup pty and start reexec process: %w", err)
			return err
		}
		defer ptm.Close()
		consoleSocket := cmd.String("console-socket")
		conn, err := net.Dial("unix", consoleSocket)
		if err != nil {
			err = fmt.Errorf("failed to dial console socket: %w", err)
			return err
		}
		defer conn.Close()

		uc, ok := conn.(*net.UnixConn)
		if !ok {
			err = fmt.Errorf("failed to cast unix socket")
			return err
		}
		defer uc.Close()

		// Send file descriptor over socket.
		oob := unix.UnixRights(int(ptm.Fd()))
		_, _, err = uc.WriteMsgUnix([]byte(ptm.Name()), oob, nil)
		if err != nil {
			err = fmt.Errorf("failed to send PTY file descriptor over socket: %w", err)
			return err
		}
	} else {
		reexecCommand.Stdin = os.Stdin
		reexecCommand.Stdout = os.Stdout
		reexecCommand.Stderr = os.Stderr
		err := reexecCommand.Start()
		if err != nil {
			err = fmt.Errorf("failed to start reexec process: %w", err)
			return err
		}
	}

	// Close child ends of sockets and pipes.
	err = initSockChild.Close()
	if err != nil {
		err = fmt.Errorf("failed to close child socket pair: %w", err)
		return err
	}
	err = logPipeChild.Close()
	if err != nil {
		err = fmt.Errorf("failed to close child log pipe: %w", err)
		return err
	}

	// Send data to nsenter
	_, err = io.Copy(initSockParent, nsenterInfo)
	if err != nil {
		err = fmt.Errorf("failed to copy nsenter info to socket: %w", err)
		return err
	}

	// Get pids from nsenter and reap dead children
	reexecPid, err := handleNsenterRet(initSockParent, reexecCommand)
	if err != nil {
		return err
	}

	if logsDone != nil {
		defer func() {
			// Wait for log forwarder to finish. This depends on
			// reexec closing the _LIBCONTAINER_LOGPIPE log fd.
			tmpErr := <-logsDone
			if tmpErr != nil && err == nil {
				err = fmt.Errorf("unable to forward init logs: %w", tmpErr)
				return
			}
		}()
	}

	// Retrieve reexec cmd's pid and write to file and state
	containerPid := reexecPid
	pidFilePath := cmd.String("pid-file")
	metrics.Capture(m.TS06)

	err = unikontainer.Create(containerPid, pidFilePath)
	if err != nil {
		return err
	}

	// execute CreateRuntime hooks
	err = unikontainer.ExecuteHooks("CreateRuntime")
	if err != nil {
		err = fmt.Errorf("failed to execute CreateRuntime hooks: %w", err)
		return err
	}
	metrics.Capture(m.TS07)

	_, err = initSockParent.Write([]byte(unikontainers.AckReexec))
	if err != nil {
		err = fmt.Errorf("failed to send ACK to reexec process: %w", err)
		return err
	}

	metrics.Capture(m.TS08)

	return err
}

func createReexecCmd(initSock *os.File, logPipe *os.File) *exec.Cmd {
	selfPath := "/proc/self/exe"
	reexecCommand := &exec.Cmd{
		Path: selfPath,
		Args: append(os.Args, "--reexec"),
		Env:  os.Environ(),
	}
	// Set files that we want to pass to children. In particular,
	// we need to pass a socketpair for the communication with the nsenter
	// and a log pipe to get logs from nsenter.
	// NOTE: Currently we only pass two files to children. In the future
	// we might need to refactor the following code, in case we need to
	// pass more than just these files.
	reexecCommand.ExtraFiles = append(reexecCommand.ExtraFiles, initSock)
	reexecCommand.ExtraFiles = append(reexecCommand.ExtraFiles, logPipe)
	// The hardcoded value here refers to the first open file descriptor after
	// the stdio file descriptors. Therefore, since the initSockChild was the
	// first file we added in ExtraFiles, its file descriptor should be 2+1=3,
	// since 0 is stdin, 1 is stdout and 2 is stderr. Similarly, the logPipeChild
	// should be right after initSockChild, hence 4
	// NOTE: THis might need bette rhandling in the future.
	reexecCommand.Env = append(reexecCommand.Env, "_LIBCONTAINER_INITPIPE=3")
	reexecCommand.Env = append(reexecCommand.Env, "_LIBCONTAINER_LOGPIPE=4")
	logLevel := strconv.Itoa(int(logrus.GetLevel()))
	if logLevel != "" {
		reexecCommand.Env = append(reexecCommand.Env, "_LIBCONTAINER_LOGLEVEL="+logLevel)
	}

	return reexecCommand
}

func handleNsenterRet(initSock *os.File, reexec *exec.Cmd) (int, error) {
	var pid struct {
		Stage2Pid int `json:"stage2_pid"`
		Stage1Pid int `json:"stage1_pid"`
	}
	decoder := json.NewDecoder(initSock)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pid); err != nil {
		return -1, fmt.Errorf("error reading pid from init pipe: %w", err)
	}

	// Clean up the zombie parent process
	Stage1Process, _ := os.FindProcess(pid.Stage1Pid)
	// Ignore the error in case the child has already been reaped for any reason
	_, _ = Stage1Process.Wait()

	status, err := reexec.Process.Wait()
	if err != nil {
		_ = reexec.Wait()
		return -1, fmt.Errorf("nsenter error: %w", err)
	}
	if !status.Success() {
		_ = reexec.Wait()
		return -1, fmt.Errorf("nsenter unsuccessful exit: %w", err)
	}

	return pid.Stage2Pid, nil
}

// reexecUnikontainer gets a Unikernel struct from state.json,
// sends ReexecStarted message to init.sock,
// waits AckReexec message on urunc.sock,
// waits StartExecve message on urunc.sock,
// executes Prestart hooks and finally execve's the unikernel vmm.
func reexecUnikontainer(cmd *cli.Command) error {
	// We need containerID here for filesystem paths.
	// Metrics get the containerID from each subcommand
	containerID := cmd.Args().First()
	metrics.SetLoggerContainerID(containerID)
	metrics.Capture(m.TS04)

	logFd, err := strconv.Atoi(os.Getenv("_LIBCONTAINER_LOGPIPE"))
	if err != nil {
		return fmt.Errorf("unable to convert _LIBCONTAINER_LOGPIPE: %w", err)
	}
	logPipe := os.NewFile(uintptr(logFd), "logpipe")
	err = logPipe.Close()
	if err != nil {
		return fmt.Errorf("close log pipe: %w", err)
	}
	initFd, err := strconv.Atoi(os.Getenv("_LIBCONTAINER_INITPIPE"))
	if err != nil {
		return fmt.Errorf("unable to convert _LIBCONTAINER_INITPIPE: %w", err)
	}
	initPipe := os.NewFile(uintptr(initFd), "initpipe")
	metrics.Capture(m.TS05)

	// wait AckReexec message on init socket from parent process
	buf := make([]byte, len(unikontainers.AckReexec))
	n, err := initPipe.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read from init socket: %w", err)
	}
	msg := string(buf[0:n])
	if msg != string(unikontainers.AckReexec) {
		return fmt.Errorf("received unexpected message: %s", msg)
	}
	err = initPipe.Close()
	if err != nil {
		return fmt.Errorf("close init pipe: %w", err)
	}
	metrics.Capture(m.TS09)

	// get Unikontainer data from state.json
	// TODO: We need to find a better way to synchronize and make sure
	// the pid is written from urunc` create. Right now we rely on receiving
	// the AckReexec message, however this is not optimal and we might lose
	// time because urunc create tries to write in a socket that the reexec
	// process has not created yet.
	unikontainer, err := getUnikontainer(cmd)
	if err != nil {
		return err
	}

	// execute CreateContainer hooks
	err = unikontainer.ExecuteHooks("CreateContainer")
	if err != nil {
		err = fmt.Errorf("failed to execute CreateContainer hooks: %w", err)
		return err
	}
	metrics.Capture(m.TS10)

	// wait StartExecve message on urunc.sock from urunc start process
	err = unikontainer.CreateListener(unikontainers.FromReexec)
	if err != nil {
		return fmt.Errorf("error creating listener on reexec socket: %w", err)
	}
	awaitErr := unikontainer.AwaitMsg(unikontainers.StartExecve)
	// Before checking for any errors, make sure to clean up the socket
	cleanErr := unikontainer.DestroyListener(unikontainers.FromReexec)
	if cleanErr != nil {
		logrus.WithError(cleanErr).Error("failed to destroy listener on reexec socket")
		cleanErr = fmt.Errorf("error destroying listener on reexec socket: %w", cleanErr)
	}
	// NOTE: Currently, we do not fail in case the socket cleanup fails
	// because urunc start has told reexec to start preparing the execution
	// environment for the monitor. Therefore, if the socket cleanup affects the
	// preparation the error will get manifested later. However, if environment
	// setup goes well and the socket was not cleaned up correctly,
	// we execve the monitor and we rely on Go's close-on-exec feature in all file
	// descriptors. THerefore, we might want to rethink this in future and not rely
	// on Go, but this requires quite a lot of changes.
	if awaitErr != nil {
		awaitErr = fmt.Errorf("error waiting START message: %w", awaitErr)
		err = errors.Join(awaitErr, cleanErr)
		return err
	}
	metrics.Capture(m.TS14)

	err = unikontainer.CreateConn(unikontainers.FromReexec)
	if err != nil {
		return fmt.Errorf("error creating connection to urunc socket: %w", err)
	}
	// NOTE: More changes are required for a proper handling of this cleanup.
	// Currently, the cleanup will happen if something fails in the container
	// setup and unikontainers.Exec returns with an error. In that case, we also
	// ignore any errors, since it is a simple socket cleanup and the process exits.
	// If unikontainers.Exec succeeds though, then we will never execute this
	// cleanup, since we execve to the monitor process. In that case, we rely
	// once more in Go's close on exec handling of all file descriptors.
	// In the future, we might want to revisit this and rely less in Go.
	defer func() {
		tmpErr := unikontainer.DestroyConn(unikontainers.FromReexec)
		if tmpErr != nil {
			logrus.WithError(tmpErr).Error("failed to destroy connection on reexec socket")
		}
	}()

	// execve
	// we need to pass metrics to Exec() function, as the unikontainer
	// struct does not have the part of urunc config that configures metrics
	var sockErr error
	err = unikontainer.Exec(metrics)
	if err != nil {
		logrus.WithError(err).Error("Setting up execution environment for monitor")
		sockErr = unikontainer.SendMessage(unikontainers.StartErr)
		if sockErr != nil {
			logrus.WithError(sockErr).Error("failed to send error message to urunc socket")
		}
	}

	return errors.Join(err, sockErr)
}
