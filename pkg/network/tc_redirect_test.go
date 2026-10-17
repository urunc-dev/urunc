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

package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

// filterCall records one redirectFilterAdd attempt by link index.
type filterCall struct {
	kind string
	src  int
	dst  int
}

// stubFilterAdd makes redirectFilterAdd record every attempt and return
// fail[kind] (nil when absent) for the rest of the test.
func stubFilterAdd(t *testing.T, fail map[string]error) *[]filterCall {
	t.Helper()
	calls := &[]filterCall{}
	orig := redirectFilterAdd
	redirectFilterAdd = func(kind string, source, target netlink.Link) error {
		*calls = append(*calls, filterCall{kind: kind, src: source.Attrs().Index, dst: target.Attrs().Index})
		return fail[kind]
	}
	t.Cleanup(func() { redirectFilterAdd = orig })
	return calls
}

func testTap() netlink.Link {
	return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Index: 7, Name: "tap0_urunc"}}
}

func testEth() netlink.Link {
	return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Index: 3, Name: "eth0"}}
}

func TestStubFilterAddIsUsed(t *testing.T) {
	calls := stubFilterAdd(t, nil)

	kind, err := addRedirectFilter(testTap(), testEth(), []string{"u32"})

	require.NoError(t, err)
	assert.Equal(t, "u32", kind)
	assert.Equal(t, []filterCall{{kind: "u32", src: 7, dst: 3}}, *calls)
}
