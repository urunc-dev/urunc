// Copyright (c) 2023-2025, Nubificus LTD
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
	"errors"
	"io"
	"log"
	"log/syslog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	lSyslog "github.com/sirupsen/logrus/hooks/syslog"
	"github.com/urunc-dev/urunc/internal/constants"
	m "github.com/urunc-dev/urunc/internal/metrics"

	_ "github.com/opencontainers/runc/libcontainer/nsenter"
	"github.com/urfave/cli"
)

const (
	specConfig = "config.json"
	usage      = `Open Container Initiative runtime

urunc is a command line client for running unikernel applications packaged according to
the Open Container Initiative (OCI) format and is a compliant implementation of the
Open Container Initiative specification.

Unikernel images are configured using bundles. A bundle for a unikernel is a directory
that includes a specification file named "` + specConfig + `" and a root filesystem.
The root filesystem contains the unikernel and any additional files required to run.

To start a new instance of a unikernel:

    # urunc run [ -b bundle ] <unikernel-id>

Where "<unikernel-id>" is your name for the instance of the unikernel that you
are starting. The name you provide for the unikernel instance must be unique on
your host. Providing the bundle directory using "-b" is optional. The default
value for "bundle" is the current directory.`
)

var version string

// FIXME: We need to find a way to set the output file
var metrics = m.NewZerologMetrics(constants.TimestampTargetFile)

func main() {
	root := "/run/urunc"
	app := cli.NewApp()
	app.Name = "urunc"
	app.Usage = usage
	app.Version = version
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug",
			Usage: "enable debug logging",
		},
		cli.StringFlag{
			Name:  "log",
			Value: "",
			Usage: "set the log file to write runc logs to (default is '/dev/stderr')",
		},
		cli.StringFlag{
			Name:  "log-format",
			Value: "text",
			Usage: "set the log format ('text' (default), or 'json')",
		},
		cli.StringFlag{
			Name:  "root",
			Value: root,
			Usage: "root directory for storage of container state (this should be located in tmpfs)",
		},
		cli.BoolFlag{
			Name:  "systemd-cgroup",
			Usage: "enable systemd cgroup support, expects cgroupsPath to be of form \"slice:prefix:name\" for e.g. \"system.slice:runc:434234\"",
		},
		cli.StringFlag{
			Name:  "rootless",
			Value: "auto",
			Usage: "ignore cgroup permission errors ('true', 'false', or 'auto')",
		},
	}
	app.Commands = []cli.Command{
		createCommand,
		deleteCommand,
		killCommand,
		runCommand,
		// specCommand,
		startCommand,
		// stateCommand,
	}
	app.Before = func(context *cli.Context) error {
		if err := reviseRootDir(context); err != nil {
			return err
		}
		return configLogrus(context)
	}

	// If the command returns an error, cli takes upon itself to print
	// the error on cli.ErrWriter and exit.
	// Use our own writer here to ensure the log gets sent to the right location.
	cli.ErrWriter = &FatalWriter{cli.ErrWriter}

	if err := app.Run(os.Args); err != nil {
		fatal(err)
	}
}

type FatalWriter struct {
	cliErrWriter io.Writer
}

func (f *FatalWriter) Write(p []byte) (n int, err error) {
	logrus.Error(string(p))
	if !logrusToStderr() {
		return f.cliErrWriter.Write(p)
	}
	return len(p), nil
}

// reviseRootDir ensures that the --root option argument,
// if specified, is converted to an absolute and cleaned path,
// and that this path is sane.
func reviseRootDir(context *cli.Context) error {
	if !context.IsSet("root") {
		return nil
	}
	root, err := filepath.Abs(context.GlobalString("root"))
	if err != nil {
		return err
	}
	if root == "/" {
		// This can happen if --root argument is
		//  - "" (i.e. empty);
		//  - "." (and the CWD is /);
		//  - "../../.." (enough to get to /);
		//  - "/" (the actual /).
		return errors.New("option --root argument should not be set to /")
	}

	return context.GlobalSet("root", root)
}

func configLogrus(context *cli.Context) error {
	if context.GlobalBool("debug") {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.SetReportCaller(true)
		// Shorten function and file names reported by the logger, by
		// trimming common "github.com/opencontainers/runc" prefix.
		// This is only done for text formatter.
		_, file, _, _ := runtime.Caller(0)
		prefix := filepath.Dir(file) + "/"
		logrus.SetFormatter(&logrus.TextFormatter{
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				function := strings.TrimPrefix(f.Function, prefix) + "()"
				fileLine := strings.TrimPrefix(f.File, prefix) + ":" + strconv.Itoa(f.Line)
				return function, fileLine
			},
		})

		// If debug is enabled, add a syslog hook for easier debugging
		hook, err := lSyslog.NewSyslogHook("", "", syslog.LOG_DEBUG, "")
		if err != nil {
			log.Fatal(err)
		}
		logrus.AddHook(hook)
	}

	switch f := context.GlobalString("log-format"); f {
	case "":
		// do nothing
	case "text":
		// do nothing
	case "json":
		logrus.SetFormatter(new(logrus.JSONFormatter))
	default:
		return errors.New("invalid log-format: " + f)
	}

	if file := context.GlobalString("log"); file != "" {
		f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_SYNC, 0o644)
		if err != nil {
			return err
		}
		logrus.SetOutput(f)
	}

	return nil
}
