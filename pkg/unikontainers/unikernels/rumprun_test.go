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

	"github.com/stretchr/testify/assert"
)

func TestRumprunCommandStringNoHTMLEscape(t *testing.T) {
	r := &Rumprun{
		Command: "app a&b <in >out",
		Envs:    []string{"FOO=x&y<z>"},
		Blk: RumprunBlk{
			Source:     "etfs",
			Path:       "/dev/ld0a",
			FsType:     "blk",
			Mountpoint: "/data&<>",
		},
	}

	got, err := r.CommandString()
	assert.NoError(t, err)
	want := `{"cmdline":"app a&b <in >out","env":"FOO=x&y<z>",` +
		`"blk":{"source":"etfs","path":"/dev/ld0a","fstype":"blk","mountpoint":"/data&<>"}}`
	assert.Equal(t, want, got)
}

func TestRumprunCommandStringNetAndEnvsNoHTMLEscape(t *testing.T) {
	r := &Rumprun{
		Command: "app",
		Envs:    []string{"FOO=a&b", "BAR=<c>"},
		Net: RumprunNet{
			Interface: "ukvmif0&",
			Cloner:    "True",
			Type:      "inet",
			Method:    "static",
			Address:   "10.0.0.2",
			Mask:      "24",
			Gateway:   "10.0.0.1<>",
		},
	}

	got, err := r.CommandString()
	assert.NoError(t, err)
	want := `{"cmdline":"app","env":"FOO=a&b","env":"BAR=<c>",` +
		`"net":{"if":"ukvmif0&","cloner":"True","type":"inet","method":"static",` +
		`"addr":"10.0.0.2","mask":"24","gw":"10.0.0.1<>"}}`
	assert.Equal(t, want, got)
}
