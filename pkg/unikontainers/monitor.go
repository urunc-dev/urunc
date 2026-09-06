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

package unikontainers

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	m "github.com/urunc-dev/urunc/internal/metrics"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// ExecMonitor is the entry point of the urunc process libcontainer starts
// inside the monitor's container. It reads the monitor spec, finalizes the
// environment for the monitor execution and execs it.
func ExecMonitor(metrics m.Writer) error {
	// The ReadyPipeFD is passed as ExtraFle with libcontainer, but in order to do
	// so, it gets CLOEXEC cleared. Therefore, restore it before spawning any child
	// (e.g. virtiofsd from PreStartCmd): so the child does not inherit it.
	syscall.CloseOnExec(ReadyPipeFD)
	// this function is supposed to be called after the libcontainer has created the
	// monitor container and is therefore in "/".
	const pivotedRoot = "/"

	ms, err := LoadMonitorSpec(pivotedRoot)
	if err != nil {
		sigErr := signalReady(false)
		if sigErr != nil {
			uniklog.WithError(sigErr).Error("failed to signal the failed start")
		}
		return fmt.Errorf("failed to read the monitor spec: %w", err)
	}
	metrics.Capture(m.TS14)

	err = runMonitor(metrics, ms)
	if err != nil {
		uniklog.WithError(err).Error("setting up execution environment for monitor")
		sigErr := signalReady(false)
		if sigErr != nil {
			uniklog.WithError(sigErr).Error("failed to signal the failed start")
		}

		return errors.Join(err, sigErr)
	}

	return nil
}

// runMonitor sets up the monitor's network and guest command, drops to the
// container user and execve's the monitor. Every failure returns to caller
// On success it does not return.
func runMonitor(metrics m.Writer, ms monitorSpec) error {
	// The monitor's environment is not carried in the spec: libcontainer already
	// gave this process the environment it configured for the monitor.
	ms.ExecArgs.Environment = os.Environ()

	unikernel, err := unikernels.New(ms.UnikernelType)
	if err != nil {
		return err
	}

	// TODO simplify this
	monitorsConfig := map[string]types.MonitorConfig{ms.MonitorType: ms.MonitorCfg}
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(ms.MonitorType), monitorsConfig)
	if err != nil {
		return err
	}

	netArgs, err := SetupNet(ms.NetworkType, ms.User.UID, ms.User.GID)
	if err != nil {
		return fmt.Errorf("failed to setup network: %w", err)
	}
	metrics.Capture(m.TS16)
	ms.ExecArgs.Net = netArgs
	ms.GuestParams.Net = netArgs

	ms.ExecArgs.Command, err = buildUnikernelCommand(unikernel, ms.GuestParams)
	if err != nil {
		return err
	}

	// Drop to the container user, which also clears the capabilities the tap
	// device needed. From here on the monitor runs unprivileged.
	err = setupUser(ms.User)
	if err != nil {
		return err
	}
	metrics.Capture(m.TS17)

	err = spawnProcess(ms.PreStartCmd)
	if err != nil {
		return err
	}

	execCmd, err := vmm.BuildExecCmd(ms.ExecArgs, unikernel)
	if err != nil {
		return err
	}

	// Report a successful setup to "urunc start" over the ready pipe, only after
	// the command has been built. signalReady closes the pipe, so it is not left
	// open in the monitor after the execve.
	err = signalReady(true)
	if err != nil {
		return err
	}

	return execMonitor(metrics, vmm, ms.ExecArgs, execCmd)
}
