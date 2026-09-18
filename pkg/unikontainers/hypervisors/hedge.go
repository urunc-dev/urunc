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

package hypervisors

import (
	"fmt"

	hedge "github.com/nubificus/hedge_cli/hedge_api"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	HedgeVmm         VmmType = "hedge"
	maxVMListRetries int     = 20
	ConsoleEndpoint          = "/proc/vmcons"
)

type Hedge struct {
	binary     string
	binaryPath string
}

func (h *Hedge) Ok() error {
	if err := hedge.Status(); err != nil {
		return ErrVMMNotInstalled
	}
	return nil
}

func (h *Hedge) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

func (h *Hedge) Stop(pid int) error {
	return killProcess(pid)
}

func (h *Hedge) UsesKVM() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (h *Hedge) SupportsSharedfs(_ string) bool {
	return false
}

func (h *Hedge) Path() string {
	if h.binaryPath != "" {
		return h.binaryPath
	}
	return hedge.MONITOR_ENDPOINT
}

func (h *Hedge) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	memMB := int(bytesToMB(args.MemSizeB))
	if memMB <= 0 {
		memMB = int(DefaultMemory)
	}

	netDev := ""
	if args.Net.TapDev != "" {
		netDev = ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
	}

	blkDev := ""
	bArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range bArgs {
		if blkDev != "" {
			blkDev += " "
		}
		blkDev += "--block:" + blockArg.ID + "=" + blockArg.Path
	}

	conf := hedge.VMConfig{
		Name:    args.ContainerID,
		Binary:  args.UnikernelPath,
		CPU:     int(args.VCPUs),
		Mem:     memMB,
		Blk:     blkDev,
		Net:     netDev,
		CmdLine: args.Command,
	}

	if err := conf.Validate(); err != nil {
		return nil, fmt.Errorf("hedge vm config validation failed: %w", err)
	}

	execCmd := []string{
		"hedge",
		"--name", conf.Name,
		"--binary", conf.Binary,
		"--cpu", fmt.Sprintf("%d", conf.CPU),
		"--mem", fmt.Sprintf("%d", conf.Mem),
		"--cmdline", conf.CmdLine,
	}
	if conf.Net != "" {
		execCmd = append(execCmd, "--net", conf.Net)
	}
	if conf.Blk != "" {
		execCmd = append(execCmd, "--blk", conf.Blk)
	}

	return execCmd, nil
}

// PreExec performs pre-execution setup for Hedge using hedge_api.
func (h *Hedge) PreExec(args types.ExecArgs) error {
	memMB := int(bytesToMB(args.MemSizeB))
	if memMB <= 0 {
		memMB = int(DefaultMemory)
	}

	netDev := ""
	if args.Net.TapDev != "" {
		netDev = args.Net.TapDev
	}

	blkDev := ""

	conf := hedge.VMConfig{
		Name:    args.ContainerID,
		Binary:  args.UnikernelPath,
		CPU:     int(args.VCPUs),
		Mem:     memMB,
		Blk:     blkDev,
		Net:     netDev,
		CmdLine: args.Command,
	}

	if err := conf.Validate(); err != nil {
		return fmt.Errorf("hedge vm config validation failed: %w", err)
	}

	return hedge.StartVM(conf)
}

func (h *Hedge) VMState(name string) string {
	vms, err := hedge.ListVMs()
	if err != nil {
		return "error"
	}
	for _, vm := range vms {
		if vm.Name == name {
			return "running"
		}
	}
	return "unknown"
}
