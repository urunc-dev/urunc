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
	"path/filepath"
	"strings"
	"syscall"

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

// Critical system paths that should never be deleted
var criticalPaths = []string{
	"/",
	"/bin",
	"/boot",
	"/dev",
	"/etc",
	"/home",
	"/lib",
	"/lib64",
	"/opt",
	"/proc",
	"/root",
	"/sbin",
	"/sys",
	"/usr",
	"/var",
}

// validateContainerID checks if the container ID is safe and doesn't contain path traversal
func validateContainerID(containerID string) error {
	// Check for empty ID
	if containerID == "" {
		return ErrEmptyContainerID
	}

	// Check for path traversal patterns
	if strings.Contains(containerID, "..") {
		return fmt.Errorf("container ID contains path traversal sequence (..): %s", containerID)
	}

	// Check for absolute paths
	if filepath.IsAbs(containerID) {
		return fmt.Errorf("container ID must not be an absolute path: %s", containerID)
	}

	// Check for directory separators (should be a simple name)
	if strings.ContainsAny(containerID, "/\\") {
		return fmt.Errorf("container ID must not contain path separators: %s", containerID)
	}

	return nil
}

// validateDeletionPath ensures a path is safe to delete
func validateDeletionPath(targetPath, rootDir string) error {
	// Clean and make absolute
	targetPath = filepath.Clean(targetPath)
	rootDir = filepath.Clean(rootDir)

	// Ensure both are absolute paths
	if !filepath.IsAbs(targetPath) {
		return fmt.Errorf("target path must be absolute: %s", targetPath)
	}
	if !filepath.IsAbs(rootDir) {
		return fmt.Errorf("root directory must be absolute: %s", rootDir)
	}

	// Check if target is within rootDir
	relPath, err := filepath.Rel(rootDir, targetPath)
	if err != nil {
		return fmt.Errorf("failed to compute relative path: %w", err)
	}

	// If relative path starts with "..", it's outside rootDir
	if strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("target path %s is outside root directory %s", targetPath, rootDir)
	}

	// Check against critical system paths
	for _, criticalPath := range criticalPaths {
		if targetPath == criticalPath {
			return fmt.Errorf("refusing to delete critical system path: %s", targetPath)
		}
		// Also check if target path is a parent of critical path
		if strings.HasPrefix(criticalPath, targetPath+string(filepath.Separator)) {
			return fmt.Errorf("target path %s contains critical system path %s", targetPath, criticalPath)
		}
	}

	return nil
}
