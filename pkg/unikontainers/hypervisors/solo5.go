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

import "github.com/urunc-dev/urunc/pkg/unikontainers/types"

// solo5ExecCmd builds the command line of a solo5 tender (hvt or spt). Both
// share the cli grammar "[ OPTIONS ] [ -- ] KERNEL [ ARGS ]".
func solo5ExecCmd(binaryPath string, args types.ExecArgs, ukernel types.Unikernel) []string {
	exArgs := []string{binaryPath, "--mem=" + BytesToStringMB(args.MemSizeB)}
	if args.Net.TapDev != "" {
		netCli := ukernel.MonitorNetCli(args.Net.TapDev, args.Net.MAC)
		exArgs = append(exArgs, netCli...)
	}
	bArgs := ukernel.MonitorBlockCli()
	for _, blockArg := range bArgs {
		if blockArg.Path != "" {
			exArgs = append(exArgs, "--block:"+blockArg.ID+"="+blockArg.Path)
		}
	}
	extraMonArgs := ukernel.MonitorCli()
	exArgs = append(exArgs, extraMonArgs.OtherArgs...)
	exArgs = append(exArgs, solo5ArgsSeparator, args.UnikernelPath)
	if args.Command != "" {
		exArgs = append(exArgs, args.Command)
	}
	return exArgs
}
