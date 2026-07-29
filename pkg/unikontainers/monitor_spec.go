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
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// monitorSpec contains the information for the urunc reexec process to
// finalize the monitor's process execution environment and exec the monitor.
type monitorSpec struct {
	ContainerID   string                `json:"containerID"`
	UnikernelType string                `json:"unikernelType"`
	MonitorType   string                `json:"monitorType"`
	MonitorCfg    types.MonitorConfig   `json:"monitorCfg"`
	ExecArgs      types.ExecArgs        `json:"execArgs"`
	GuestParams   types.UnikernelParams `json:"guestParams"`
}
