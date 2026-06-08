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
	"os/exec"
	"syscall"

	"github.com/moby/sys/userns"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	"github.com/urunc-dev/urunc/pkg/unikontainers"
	"golang.org/x/sys/unix"
)

// Argument check types for the `checkArgs` function.
const (
	exactArgs = iota // Checks for an exact number of arguments.
	minArgs          // Checks for a minimum number of arguments.
	maxArgs          // Checks for a maximum number of arguments.
)

var ErrEmptyContainerID = errors.New("container ID can not be empty")

// checkArgs checks the number of arguments provided in the command-line context
// against the expected number, based on the specified checkType.
func checkArgs(cmd *cli.Command, expected, checkType int) error {
	var err error
	cmdName := cmd.Name

	switch checkType {
	case exactArgs:
		if cmd.NArg() != expected {
			err = fmt.Errorf("%s: %q requires exactly %d argument(s)", os.Args[0], cmdName, expected)
		}
	case minArgs:
		if cmd.NArg() < expected {
			err = fmt.Errorf("%s: %q requires a minimum of %d argument(s)", os.Args[0], cmdName, expected)
		}
	case maxArgs:
		if cmd.NArg() > expected {
			err = fmt.Errorf("%s: %q requires a maximum of %d argument(s)", os.Args[0], cmdName, expected)
		}
	}

	if err != nil {
		fmt.Printf("Incorrect Usage.\n\n")
		_ = cli.ShowCommandHelp(context.Background(), cmd, cmdName)
		return err
	}
	return nil
}

func getUnikontainer(cmd *cli.Command) (*unikontainers.Unikontainer, error) {
	containerID := cmd.Args().First()
	if containerID == "" {
		return nil, ErrEmptyContainerID
	}

	// We have already made sure in main.go that root is not nil
	rootDir := cmd.String("root")

	// get Unikontainer data from state.json
	unikontainer, err := unikontainers.Get(containerID, rootDir)
	if err != nil {
		if errors.Is(err, unikontainers.ErrNotUnikernel) {
			// Exec runc to handle non unikernel containers
			// It should never return
			err = runcExec()
			return nil, err
		}
		return nil, err
	}

	return unikontainer, nil
}

func runcExec() error {
	args := os.Args
	binPath, err := exec.LookPath("runc")
	if err != nil {
		return err
	}
	args[0] = binPath
	return syscall.Exec(binPath, args, os.Environ())
}

// newSockPair returns a new SOCK_STREAM unix socket pair.
func newSockPair(name string) (parent, child *os.File, err error) {
	fds, err := unix.Socketpair(unix.AF_LOCAL, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	parent = os.NewFile(uintptr(fds[1]), name+"-p")
	child = os.NewFile(uintptr(fds[0]), name+"-c")
	return parent, child, nil
}

func logrusToStderr() bool {
	l, ok := logrus.StandardLogger().Out.(*os.File)
	return ok && l.Fd() == os.Stderr.Fd()
}

// fatal prints the error's details if it is a libcontainer specific error type
// then exits the program with an exit status of 1.
func fatal(err error) {
	fatalWithCode(err, 1)
}

func fatalWithCode(err error, ret int) {
	// Make sure the error is written to the logger.
	logrus.Error(err)
	if !logrusToStderr() {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(ret)
}

// ShouldHonorXDGRuntimeDir reports whether the runtime should use XDG_RUNTIME_DIR
// for the default root directory (e.g. /run/user/UID/urunc instead of /run/urunc).
// It returns true for non-root processes and for root inside a user namespace
// when not running as the "root" user (e.g. rootless Podman).
func ShouldHonorXDGRuntimeDir() bool {
	if os.Geteuid() != 0 {
		return true
	}
	if !userns.RunningInUserNS() {
		// euid == 0 in the initial ns (real root): use /run/urunc for backward compatibility.
		return false
	}
	// euid == 0 inside a user namespace (rootless): honor XDG_RUNTIME_DIR unless USER=root.
	u, ok := os.LookupEnv("USER")
	return !ok || u != "root"
}

// prepareXDGRuntimeDir prepares the XDG_RUNTIME_DIR directory according to the XDG specification.
// It creates the directory with proper permissions and sets the sticky bit to prevent auto-pruning.
func prepareXDGRuntimeDir(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("the path in $XDG_RUNTIME_DIR must be writable by the user: %w", err)
	}
	if err := os.Chmod(root, os.FileMode(0o700)|os.ModeSticky); err != nil {
		return fmt.Errorf("you should check permission of the path in $XDG_RUNTIME_DIR: %w", err)
	}
	return nil
}
