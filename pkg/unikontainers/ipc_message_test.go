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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

func TestInt32msgSerialize(t *testing.T) {
	native := nl.NativeEndian()
	tests := []struct {
		name    string
		msgType uint16
		value   uint32
	}{
		{
			name:    "clone flags attribute with typical value",
			msgType: cloneFlagsAttr,
			value:   unix.CLONE_NEWNET | unix.CLONE_NEWPID,
		},
		{
			name:    "zero value",
			msgType: cloneFlagsAttr,
			value:   0,
		},
		{
			name:    "max uint32 value",
			msgType: oomScoreAdjAttr,
			value:   ^uint32(0),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := &int32msg{Type: tt.msgType, Value: tt.value}
			got := msg.Serialize()
			assert.Equal(t, 8, len(got))
			assert.Equal(t, uint16(8), native.Uint16(got[0:2]))
			assert.Equal(t, tt.msgType, native.Uint16(got[2:4]))
			assert.Equal(t, tt.value, native.Uint32(got[4:8]))
		})
	}
}

func TestBytemsgSerialize(t *testing.T) {
	native := nl.NativeEndian()
	tests := []struct {
		name      string
		msgType   uint16
		value     []byte
		wantPanic bool
	}{
		{
			name:    "empty value produces aligned buffer",
			msgType: nsPathsAttr,
			value:   []byte{},
		},
		{
			name:    "non-empty value encodes correctly",
			msgType: nsPathsAttr,
			value:   []byte("net:/tmp/netns"),
		},
		{
			name:      "oversized value panics with netlinkError",
			msgType:   nsPathsAttr,
			value:     make([]byte, 65532),
			wantPanic: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := &bytemsg{Type: tt.msgType, Value: tt.value}
			if tt.wantPanic {
				assert.Panics(t, func() { msg.Serialize() })
				return
			}
			got := msg.Serialize()
			l := msg.Len()
			expectedLen := (l + unix.NLA_ALIGNTO - 1) &^ (unix.NLA_ALIGNTO - 1)
			assert.Equal(t, expectedLen, len(got))
			assert.Equal(t, uint16(l), native.Uint16(got[0:2])) //nolint: gosec
			assert.Equal(t, tt.msgType, native.Uint16(got[2:4]))
			if len(tt.value) > 0 {
				assert.Equal(t, tt.value, got[unix.NLA_HDRLEN:unix.NLA_HDRLEN+len(tt.value)])
			}
		})
	}
}
