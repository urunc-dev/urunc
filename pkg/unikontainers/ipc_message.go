//go:build linux
// +build linux

// The below code is taken from runc's libcontainer/message_linux.go
// Commit: ba0b5e2 // spellchecker:disable-line
// The code is shipped under the Apache 2.0 License. For more information
// regarding licensing for this particular file and authors, please
// check runc's repository: https://github.com/opencontainers/runc
//
// There are a few changes made to the code, which follow the
// same exact license with runc and urunc.

package unikontainers

import (
	"fmt"
	"math"

	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

// list of known message types we want to send to bootstrap program
// The number is randomly chosen to not conflict with known netlink types
const (
	initMsg          uint16 = 62000
	cloneFlagsAttr   uint16 = 27281
	nsPathsAttr      uint16 = 27282
	uidmapAttr       uint16 = 27283
	gidmapAttr       uint16 = 27284
	setgroupAttr     uint16 = 27285
	oomScoreAdjAttr  uint16 = 27286
	rootlessEUIDAttr uint16 = 27287
	uidmapPathAttr   uint16 = 27288
	gidmapPathAttr   uint16 = 27289
	timeOffsetsAttr  uint16 = 27290
)

// netlinkError is an error wrapper type for use by custom netlink message
// types. Panics with errors are wrapped in netlinkError so that the recover
// in bootstrapData can distinguish intentional panics.
type netlinkError struct{ error }

type int32msg struct {
	Type  uint16
	Value uint32
}

// serialize serializes the message.
// int32msg has the following representation
// | nlattr len | nlattr type |
// | uint32 value             |
func (msg *int32msg) Serialize() []byte {
	buf := make([]byte, msg.Len())
	native := nl.NativeEndian()
	native.PutUint16(buf[0:2], uint16(msg.Len())) //nolint: gosec
	native.PutUint16(buf[2:4], msg.Type)
	native.PutUint32(buf[4:8], msg.Value)
	return buf
}

func (msg *int32msg) Len() int {
	return unix.NLA_HDRLEN + 4
}

// bytemsg has the following representation
// | nlattr len | nlattr type |
// | value              | pad |
type bytemsg struct {
	Type  uint16
	Value []byte
}

func (msg *bytemsg) Serialize() []byte {
	l := msg.Len()
	if l > math.MaxUint16 {
		// We cannot return nil nor an error here, so we panic with
		// a specific type instead, which is handled via recover in
		// bootstrapData.
		panic(netlinkError{fmt.Errorf("netlink: cannot serialize bytemsg of length %d (larger than UINT16_MAX)", l)})
	}
	buf := make([]byte, (l+unix.NLA_ALIGNTO-1) & ^(unix.NLA_ALIGNTO-1))
	native := nl.NativeEndian()
	native.PutUint16(buf[0:2], uint16(l)) //nolint: gosec
	native.PutUint16(buf[2:4], msg.Type)
	copy(buf[4:], msg.Value)
	return buf
}

func (msg *bytemsg) Len() int {
	return unix.NLA_HDRLEN + len(msg.Value) + 1 // null-terminated
}
