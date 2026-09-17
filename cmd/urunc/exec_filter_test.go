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

package main

import (
	"strings"
	"testing"
)

func filter(s string) string {
	out, _ := sanitizeTerminalBytes([]byte(s))
	return string(out)
}

// TestTerminalFilterAllows checks that ordinary TUI output is passed through
// untouched: colours, cursor movement, box-drawing charset, title-stack, and a
// clipboard *set* (how TUIs copy).
func TestTerminalFilterAllows(t *testing.T) {
	allowed := []struct{ name, seq string }{
		{"plain text", "hello world\n"},
		{"sgr colour", "\x1b[31mred\x1b[0m"},
		{"cursor move", "\x1b[2J\x1b[H"},
		{"box drawing charset (ESC ( 0)", "\x1b(0lqk\x1b(B"},
		{"title stack push/pop", "\x1b[22;0t\x1b[23;0t"},
		{"osc 52 clipboard set", "\x1b]52;c;aGVsbG8=\x07"},
		{"bracketed paste", "\x1b[?2004h"},
		{"utf-8 runes", "héllo — 世界"},
	}
	for _, a := range allowed {
		if got := filter(a.seq); got != a.seq {
			t.Errorf("%s: filtered %q -> %q, want unchanged", a.name, a.seq, got)
		}
	}
}

// TestTerminalFilterBlocks checks that reply-soliciting and reach-beyond
// sequences are dropped while surrounding text survives.
func TestTerminalFilterBlocks(t *testing.T) {
	blocked := []struct{ name, seq, mustNotContain string }{
		{"osc 52 clipboard query", "A\x1b]52;c;?\x07B", "52"},
		{"cursor position report", "A\x1b[6nB", "["},
		{"primary device attributes", "A\x1b[cB", "\x1b"},
		{"device status report", "A\x1b[5nB", "\x1b"},
		{"xtversion (CSI > q)", "A\x1b[>qB", "\x1b"},
		{"osc 1337 iterm channel", "A\x1b]1337;foo\x07B", "1337"},
		{"palette query (OSC 10 ?)", "A\x1b]10;?\x07B", "\x1b"},
		{"8-bit CSI introducer", "A\x9b6nB", "6n"},
		{"8-bit OSC clipboard query", "A\x9d52;c;?\x07B", "52"},
		{"leave utf-8 (ESC % @)", "A\x1b%@B", "\x1b"},
		{"DCS string", "A\x1bPq...\x1b\\B", "q..."},
	}
	for _, b := range blocked {
		got := filter(b.seq)
		if strings.Contains(got, b.mustNotContain) {
			t.Errorf("%s: filtered %q -> %q, still contains %q", b.name, b.seq, got, b.mustNotContain)
		}
		// The surrounding plain text A/B must survive.
		if !strings.Contains(got, "A") || !strings.Contains(got, "B") {
			t.Errorf("%s: filtered %q -> %q, dropped surrounding text", b.name, b.seq, got)
		}
	}
}

// TestTerminalFilterReassembles checks a sequence split across two writes is
// judged whole, not passed as a fragment (the classic smuggling vector).
func TestTerminalFilterReassembles(t *testing.T) {
	san := &terminalSanitizer{w: &strings.Builder{}}
	buf := san.w.(*strings.Builder)

	// A cursor-position-report split mid-sequence: "\x1b[6" then "n".
	_, _ = san.Write([]byte("start\x1b[6"))
	_, _ = san.Write([]byte("nend"))
	got := buf.String()
	if strings.Contains(got, "6n") || strings.Contains(got, "\x1b") {
		t.Errorf("split CSI leaked through reassembly: %q", got)
	}
	if !strings.Contains(got, "start") || !strings.Contains(got, "end") {
		t.Errorf("reassembly dropped surrounding text: %q", got)
	}
}

// TestTerminalFilterEscapeSmuggle covers the tmux-style trick of hiding an OSC
// after a CSI that ends at an ESC: the fragment must be dropped and the stream
// must resynchronise on the OSC, which is then judged as the OSC it is.
func TestTerminalFilterEscapeSmuggle(t *testing.T) {
	// ESC [ ESC ] 52;c;? BEL : a CSI abandoned at the ESC, then a clipboard
	// query. Neither may survive.
	got := filter("\x1b[\x1b]52;c;?\x07")
	if strings.Contains(got, "52") || strings.Contains(got, "\x1b") {
		t.Errorf("escape-smuggled clipboard query leaked: %q", got)
	}
}
