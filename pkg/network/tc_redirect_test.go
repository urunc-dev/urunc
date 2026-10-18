//go:build linux

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
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
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

// stubFilterAddFunc makes redirectFilterAdd record every attempt and return
// what fail says for it, for the rest of the test.
func stubFilterAddFunc(t *testing.T, fail func(kind string, src int) error) *[]filterCall {
	t.Helper()
	calls := &[]filterCall{}
	orig := redirectFilterAdd
	redirectFilterAdd = func(kind string, source, target netlink.Link) error {
		*calls = append(*calls, filterCall{kind: kind, src: source.Attrs().Index, dst: target.Attrs().Index})
		return fail(kind, source.Attrs().Index)
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

func TestRedirectClassifierOrder(t *testing.T) {
	assert.Equal(t, []string{"u32", "matchall", "flower", "basic"}, redirectClassifiers)
}

func TestAddRedirectFilterFallbackOrder(t *testing.T) {
	calls := stubFilterAdd(t, map[string]error{
		"u32":      unix.ENOENT,
		"matchall": unix.ENOENT,
	})

	kind, err := addRedirectFilter(testTap(), testEth(), redirectClassifiers)

	require.NoError(t, err)
	assert.Equal(t, "flower", kind)
	assert.Equal(t, []filterCall{
		{kind: "u32", src: 7, dst: 3},
		{kind: "matchall", src: 7, dst: 3},
		{kind: "flower", src: 7, dst: 3},
	}, *calls)
}

func TestAddRedirectFilterAllRefused(t *testing.T) {
	calls := stubFilterAdd(t, map[string]error{
		"u32":      unix.ENOENT,
		"matchall": unix.ENOENT,
		"flower":   unix.EOPNOTSUPP,
		"basic":    unix.EINVAL,
	})

	kind, err := addRedirectFilter(testTap(), testEth(), redirectClassifiers)

	require.Error(t, err)
	assert.Empty(t, kind)
	assert.Contains(t, err.Error(), "no usable tc classifier for tap0_urunc->eth0")
	for _, k := range redirectClassifiers {
		assert.Contains(t, err.Error(), k)
	}
	assert.True(t, errors.Is(err, unix.ENOENT))
	assert.True(t, errors.Is(err, unix.EINVAL))
	assert.Len(t, *calls, 4)
}

func TestAddRedirectFiltersReusesKind(t *testing.T) {
	calls := stubFilterAdd(t, map[string]error{"u32": unix.ENOENT})

	kind, err := addRedirectFilters(testTap(), testEth(), redirectClassifiers)

	require.NoError(t, err)
	assert.Equal(t, "matchall", kind)
	assert.Equal(t, []filterCall{
		{kind: "u32", src: 7, dst: 3},
		{kind: "matchall", src: 7, dst: 3},
		{kind: "matchall", src: 3, dst: 7},
	}, *calls)
}

func TestAddRedirectFiltersSecondDirectionFails(t *testing.T) {
	calls := stubFilterAddFunc(t, func(kind string, src int) error {
		if kind == "u32" || (kind == "matchall" && src == 3) {
			return unix.ENOENT
		}
		return nil
	})

	kind, err := addRedirectFilters(testTap(), testEth(), redirectClassifiers)

	require.Error(t, err)
	assert.Empty(t, kind)
	assert.Contains(t, err.Error(), "addRedirectFilter(eth0->tap0_urunc) failed")
	assert.True(t, errors.Is(err, unix.ENOENT))
	assert.Equal(t, []filterCall{
		{kind: "u32", src: 7, dst: 3},
		{kind: "matchall", src: 7, dst: 3},
		{kind: "matchall", src: 3, dst: 7},
	}, *calls)
	for _, c := range *calls {
		assert.NotEqual(t, "flower", c.kind)
	}
}

func TestAddRedirectFilterNoKinds(t *testing.T) {
	calls := stubFilterAdd(t, nil)

	kind, err := addRedirectFilter(testTap(), testEth(), nil)

	require.Error(t, err)
	assert.Empty(t, kind)
	assert.Empty(t, *calls)
}

func TestNetlinkRedirectFilterAddUnknownKind(t *testing.T) {
	err := netlinkRedirectFilterAdd("nonexistent", testTap(), testEth())

	require.Error(t, err)
	assert.EqualError(t, err, `unknown tc classifier "nonexistent"`)
}

func TestTcIfindexRange(t *testing.T) {
	_, err := tcIfindex(-1)
	assert.Error(t, err)

	_, err = tcIfindex(math.MaxInt32 + 1)
	assert.Error(t, err)

	idx, err := tcIfindex(3)
	require.NoError(t, err)
	assert.Equal(t, int32(3), idx)
}
