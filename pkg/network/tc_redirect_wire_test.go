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
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

// assertRedirectActions checks the single mirred action: steal the packet and
// redirect it to the egress of the link with index ifindex.
func assertRedirectActions(t *testing.T, actions []netlink.Action, ifindex int) {
	t.Helper()
	require.Len(t, actions, 1)
	mirred, ok := actions[0].(*netlink.MirredAction)
	require.True(t, ok, "action is %T", actions[0])
	assert.Equal(t, netlink.TCA_EGRESS_REDIR, mirred.MirredAction)
	assert.Equal(t, netlink.TC_ACT_STOLEN, mirred.Attrs().Action)
	assert.Equal(t, ifindex, mirred.Ifindex)
}

func TestRedirectFilterAttrs(t *testing.T) {
	attrs := redirectFilterAttrs(testTap())

	assert.Equal(t, 7, attrs.LinkIndex)
	assert.Equal(t, netlink.MakeHandle(0xffff, 0), attrs.Parent)
	assert.Equal(t, uint16(unix.ETH_P_ALL), attrs.Protocol)
}

func TestRedirectFilterKindsMatchAllAndRedirect(t *testing.T) {
	for _, dir := range []struct {
		name         string
		source, dest netlink.Link
	}{
		{"tap to eth", testTap(), testEth()},
		{"eth to tap", testEth(), testTap()},
	} {
		for _, kind := range []string{"u32", "matchall", "flower"} {
			t.Run(dir.name+"/"+kind, func(t *testing.T) {
				filter, err := redirectFilter(kind, dir.source, dir.dest)
				require.NoError(t, err)

				assert.Equal(t, kind, filter.Type())
				assert.Equal(t, redirectFilterAttrs(dir.source), *filter.Attrs())
				switch f := filter.(type) {
				case *netlink.U32:
					assertRedirectActions(t, f.Actions, dir.dest.Attrs().Index)
				case *netlink.MatchAll:
					assertRedirectActions(t, f.Actions, dir.dest.Attrs().Index)
				case *netlink.Flower:
					assertRedirectActions(t, f.Actions, dir.dest.Attrs().Index)
					// A flower filter with no key matches every packet.
					assert.Equal(t, netlink.Flower{
						FilterAttrs: f.FilterAttrs,
						Actions:     f.Actions,
					}, *f)
				default:
					t.Fatalf("unexpected filter %T", filter)
				}
			})
		}
	}
}

func TestRedirectFilterUnknownKind(t *testing.T) {
	filter, err := redirectFilter("bogus", testTap(), testEth())

	assert.Nil(t, filter)
	assert.EqualError(t, err, `unknown tc classifier "bogus"`)
}

// childAttrs parses the route attributes in data.
func childAttrs(t *testing.T, data []byte) []nlAttr {
	t.Helper()
	attrs, err := nl.ParseRouteAttr(data)
	require.NoError(t, err)
	out := make([]nlAttr, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, nlAttr{typ: int(a.Attr.Type) &^ unix.NLA_F_NESTED, value: a.Value})
	}
	return out
}

type nlAttr struct {
	typ   int
	value []byte
}

func findAttr(t *testing.T, attrs []nlAttr, typ int) []byte {
	t.Helper()
	for _, a := range attrs {
		if a.typ == typ {
			return a.value
		}
	}
	t.Fatalf("attribute %d not found", typ)
	return nil
}

func TestBasicRedirectRequestLayout(t *testing.T) {
	source, target := testEth(), testTap()

	req, err := basicRedirectRequest(redirectFilterAttrs(source), redirectActions(target))
	require.NoError(t, err)
	data := req.Serialize()

	// Netlink header, then the tc message, then the attributes.
	const hdr = unix.SizeofNlMsghdr
	const tcLen = nl.SizeofTcMsg
	require.Greater(t, len(data), hdr+tcLen)
	assert.Equal(t, uint16(unix.RTM_NEWTFILTER), binary.NativeEndian.Uint16(data[4:6]))
	msg := nl.DeserializeTcMsg(data[hdr : hdr+tcLen])
	assert.EqualValues(t, source.Attrs().Index, msg.Ifindex)
	assert.Equal(t, netlink.MakeHandle(0xffff, 0), msg.Parent)
	assert.Equal(t, netlink.MakeHandle(0, nl.Swap16(unix.ETH_P_ALL)), msg.Info)

	top := childAttrs(t, data[hdr+tcLen:])
	assert.Equal(t, nl.ZeroTerminated("basic"), findAttr(t, top, nl.TCA_KIND))

	// TCA_OPTIONS holds TCA_BASIC_ACT only: no match rule, so every packet
	// matches.
	options := childAttrs(t, findAttr(t, top, nl.TCA_OPTIONS))
	require.Len(t, options, 1)
	assert.Equal(t, tcaBasicAct, options[0].typ)

	// One action, numbered 1, that is a mirred redirect to the target.
	actions := childAttrs(t, options[0].value)
	require.Len(t, actions, 1)
	assert.Equal(t, 1, actions[0].typ)
	action := childAttrs(t, actions[0].value)
	assert.Equal(t, nl.ZeroTerminated("mirred"), findAttr(t, action, nl.TCA_ACT_KIND))
	mirredOpts := childAttrs(t, findAttr(t, action, nl.TCA_ACT_OPTIONS))
	parms := findAttr(t, mirredOpts, nl.TCA_MIRRED_PARMS)
	require.GreaterOrEqual(t, len(parms), nl.SizeofTcMirred)
	mirred := nl.DeserializeTcMirred(parms)
	assert.Equal(t, int32(netlink.TCA_EGRESS_REDIR), mirred.Eaction)
	assert.Equal(t, int32(netlink.TC_ACT_STOLEN), mirred.Action)
	assert.EqualValues(t, target.Attrs().Index, mirred.Ifindex)
	assert.True(t, bytes.Contains(data, []byte("mirred")))
}

func TestBasicRedirectRequestBadIfindex(t *testing.T) {
	attrs := redirectFilterAttrs(testTap())
	attrs.LinkIndex = -1

	req, err := basicRedirectRequest(attrs, redirectActions(testEth()))

	assert.Nil(t, req)
	assert.Error(t, err)
}
