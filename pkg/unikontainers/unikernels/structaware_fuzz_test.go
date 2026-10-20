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

package unikernels

import (
	"testing"

	fuzzheaders "github.com/AdaLogics/go-fuzz-headers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// allUnikernelTypes is every type unikernels.New dispatches on.
var allUnikernelTypes = []string{
	RumprunUnikernel, UnikraftUnikernel, MirageUnikernel,
	MewzUnikernel, LinuxUnikernel, HermitUnikernel,
}

// FuzzUnikernelInit: structure-aware fuzzing (GenerateStruct, like runc's
// OSS-Fuzz harnesses) drives Init and every monitor-accessor for all 6
// unikernel types. Oracle: none of it may panic. (A "CommandString must be
// non-empty" assertion was withdrawn: Mewz legitimately returns empty.)
func FuzzUnikernelInit(f *testing.F) {
	f.Add([]byte("mirage-seed-input-padding-0000000000"))
	f.Add([]byte("linux-seed-input-padding-00000000000"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 16 {
			return
		}
		c := fuzzheaders.NewConsumer(data)

		params := types.UnikernelParams{}
		if err := c.GenerateStruct(&params); err != nil {
			return
		}

		for _, ut := range allUnikernelTypes {
			u, err := New(ut)
			if err != nil {
				t.Fatalf("New(%q) failed although it is a supported type: %v", ut, err)
			}
			if err := u.Init(params); err != nil {
				continue
			}

			if _, err := u.CommandString(); err != nil {
				continue
			}

			// Accessors a monitor invocation reaches after Init.
			_ = u.SupportsBlock()
			_ = u.SupportsFS(params.Rootfs.Type)
			_ = u.MonitorNetCli(params.Net.TapDev, params.Net.MAC)
			_ = u.MonitorBlockCli()
			_ = u.MonitorCli()
		}
	})
}

// TestUnikernelNew replaces FuzzUnikernelNew: a fixed dispatcher has no
// adversarial shape, and fuzzing found nothing. A table test covers it for
// less machinery.
func TestUnikernelNew(t *testing.T) {
	for _, k := range allUnikernelTypes {
		t.Run(k, func(t *testing.T) {
			u, err := New(k)
			require.NoError(t, err)
			require.NotNil(t, u)
		})
	}

	for _, bad := range []string{"", "nonsense", "Linux", " linux"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			_, err := New(bad)
			assert.Error(t, err)
		})
	}
}
