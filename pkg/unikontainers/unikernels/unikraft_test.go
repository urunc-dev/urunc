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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestUnikraftVersionParsing(t *testing.T) {
	t.Run("empty version returns ErrUndefinedVersion", func(t *testing.T) {
		u := newUnikraft()
		err := u.Init(types.UnikernelParams{
			Version: "",
			Net:     types.NetDevParams{IP: "10.0.0.2", Gateway: "10.0.0.1", Mask: "255.255.255.0"},
		})
		assert.True(t, errors.Is(err, ErrUndefinedVersion))
	})

	t.Run("invalid version returns wrapped ErrVersionParsing with details", func(t *testing.T) {
		u := newUnikraft()
		invalidVersion := "invalid.version.string!!!"
		err := u.Init(types.UnikernelParams{
			Version: invalidVersion,
			Net:     types.NetDevParams{IP: "10.0.0.2", Gateway: "10.0.0.1", Mask: "255.255.255.0"},
		})
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrVersionParsing), "errors.Is should identify ErrVersionParsing")
		assert.Contains(t, err.Error(), ErrVersionParsing.Error())
		assert.Contains(t, err.Error(), "malformed version: invalid.version.string!!!")
	})

	t.Run("valid new version configures current args", func(t *testing.T) {
		u := newUnikraft()
		err := u.Init(types.UnikernelParams{
			Version: "0.17.0",
			Net:     types.NetDevParams{IP: "10.0.0.2", Gateway: "10.0.0.1", Mask: "255.255.255.0"},
		})
		assert.NoError(t, err)
		assert.Contains(t, u.Net.Address, "netdev.ip=")
	})

	t.Run("valid legacy version configures compat args", func(t *testing.T) {
		u := newUnikraft()
		err := u.Init(types.UnikernelParams{
			Version: "0.15.0",
			Net:     types.NetDevParams{IP: "10.0.0.2", Gateway: "10.0.0.1", Mask: "255.255.255.0"},
		})
		assert.NoError(t, err)
		assert.Contains(t, u.Net.Address, "netdev.ipv4_addr=")
	})
}
