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

type mockUnikernel struct {
	netCli   string
	blockCli []types.MonitorBlockArgs
	monCli   types.MonitorCliArgs
}

func (m *mockUnikernel) Init(_ types.UnikernelParams) error        { return nil }
func (m *mockUnikernel) CommandString() (string, error)            { return "", nil }
func (m *mockUnikernel) SupportsBlock() bool                       { return false }
func (m *mockUnikernel) SupportsFS(_ string) bool                  { return false }
func (m *mockUnikernel) MonitorNetCli(_, _ string) string          { return m.netCli }
func (m *mockUnikernel) MonitorBlockCli() []types.MonitorBlockArgs { return m.blockCli }
func (m *mockUnikernel) MonitorCli() types.MonitorCliArgs          { return m.monCli }
