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

// The guest-output terminal filter below is ported from brig-sh/hull
// (cmd/hull/exec.go) so `urunc exec` gives the same protection: everything a
// guest prints lands on the operator's terminal, and a terminal is not a
// display -- a handful of escape sequences make it *type*. The reply to such a
// sequence is written into the tty input buffer, where it is read either by the
// stdin pump (and forwarded straight back into the guest) or, once the client
// has exited, by the operator's shell. That is what turns "the sandbox printed
// something" into "the sandbox read my clipboard" (an OSC 52 query) or "the
// sandbox chose a line of text for my shell to see" (OSC 2 set title, CSI 21 t
// ask for it back).
//
// The policy is deliberately narrow rather than a blanket strip: SGR colour,
// cursor movement, scroll regions, mouse tracking, bracketed paste and the
// alternate screen all pass through untouched, so a coding-agent TUI works.
// What is dropped is the set of sequences that solicit a reply or otherwise
// reach beyond the display (clipboard, palette/geometry/identity queries, DCS
// as a class, iTerm2's channel and 8-bit C1 controls). Non-terminal
// destinations (a pipe, a file, a CI log) are never filtered.

package main

import (
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// TerminalFilterEnv turns the guest-output filter off. A control that can
// strand somebody with no way out is worse than one that can be switched off
// knowingly.
const TerminalFilterEnv = "URUNC_TERMINAL_FILTER"

// isTerminal reports whether fd refers to a terminal.
func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	return err == nil
}

func terminalFilterDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(TerminalFilterEnv))) {
	case "off", "0", "none", "false":
		return true
	}
	// Anything unrecognised leaves the filter ON: a typo must not be the thing
	// that silently disables it.
	return false
}

// guestTerminalWriter wraps f in the filter when f is an interactive terminal,
// and returns f unchanged otherwise.
func guestTerminalWriter(f *os.File) io.Writer {
	if f == nil || !isTerminal(int(f.Fd())) {
		return f
	}
	if terminalFilterDisabled() {
		return f
	}
	return &terminalSanitizer{w: f}
}

// sanitizeGuestText makes a guest-chosen string safe to put in a message the
// client prints itself. Everything unconsumed at the end is dropped: a trailing
// fragment is an incomplete escape and there is no next write to complete it.
func sanitizeGuestText(sIn string) string {
	if terminalFilterDisabled() {
		return sIn
	}
	out, _ := sanitizeTerminalBytes([]byte(sIn))
	return string(out)
}

// maxHeldEscape bounds the bytes held back waiting for an escape sequence to
// finish. A sequence split across two frames is normal and must be reassembled;
// an "OSC" with no terminator is not, and must not be able to buffer the
// session's whole output or stall it forever.
const maxHeldEscape = 4096

// terminalSanitizer applies the policy to a byte stream. It is stateful because
// escape sequences do not respect write boundaries: a CSI can arrive split
// across two agent frames.
type terminalSanitizer struct {
	w    io.Writer
	held []byte
}

func (t *terminalSanitizer) Write(p []byte) (int, error) {
	buf := p
	if len(t.held) > 0 {
		buf = append(append([]byte(nil), t.held...), p...)
	}

	out, consumed := sanitizeTerminalBytes(buf)
	rest := buf[consumed:]
	if len(rest) > maxHeldEscape {
		rest = nil
	}
	t.held = append([]byte(nil), rest...)

	if len(out) > 0 {
		if _, err := t.w.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// sanitizeTerminalBytes returns the bytes safe to emit and how much of b was
// consumed. The unconsumed tail is an escape sequence or a UTF-8 rune that is
// still incomplete, and the caller must hold it for the next write.
func sanitizeTerminalBytes(b []byte) (out []byte, consumed int) {
	out = make([]byte, 0, len(b))
	i := 0
	for i < len(b) {
		c := b[i]
		switch {
		case c == 0x1b:
			n, ok := escapeLen(b[i:])
			if !ok {
				return out, i
			}
			if seq := b[i : i+n]; escapeIsSafe(seq) {
				out = append(out, seq...)
			}
			i += n
		case c >= 0x80 && c < 0xc0:
			// Never a valid UTF-8 lead byte: a C1 control or a stray
			// continuation byte. Drop C1 introducers whole (measured in place,
			// without copying the remainder), and skip stray bytes.
			equiv, isIntroducer := c1Introducer(c)
			if !isIntroducer {
				i++
				break
			}
			n, ok := escapeLenAfterIntro(equiv, b[i+1:])
			if !ok {
				return out, i
			}
			i += n
		case c >= 0xc0:
			if !utf8.FullRune(b[i:]) {
				return out, i
			}
			_, n := utf8.DecodeRune(b[i:])
			out = append(out, b[i:i+n]...)
			i += n
		default:
			out = append(out, c)
			i++
		}
	}
	return out, i
}

// c1Introducer maps an 8-bit C1 control to the byte that follows ESC in its
// 7-bit spelling.
func c1Introducer(c byte) (byte, bool) {
	switch c {
	case 0x90:
		return 'P', true // DCS
	case 0x98:
		return 'X', true // SOS
	case 0x9b:
		return '[', true // CSI
	case 0x9d:
		return ']', true // OSC
	case 0x9e:
		return '^', true // PM
	case 0x9f:
		return '_', true // APC
	}
	return 0, false
}

// escapeLen measures the escape sequence starting at b[0] (which must be ESC),
// reporting ok=false when the sequence is not complete yet.
func escapeLen(b []byte) (int, bool) {
	if len(b) < 2 {
		return 0, false
	}
	switch b[1] {
	case '[':
		i := 2
		for i < len(b) && b[i] >= 0x30 && b[i] <= 0x3f {
			i++
		}
		for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2f {
			i++
		}
		if i >= len(b) {
			return 0, false
		}
		if b[i] == 0x1b {
			// ESC is not a final byte: a real terminal abandons this sequence.
			// Stop before the ESC so the stream resynchronises on it.
			return i, true
		}
		return i + 1, true
	case ']', 'P', 'X', '^', '_':
		return stringSeqLen(b)
	default:
		if b[1] == 0x1b {
			// ESC cancels whatever it interrupts (also how a tmux passthrough
			// smuggles a nested sequence). Consume just this byte.
			return 1, true
		}
		if b[1] >= 0x20 && b[1] <= 0x2f {
			i := 2
			for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2f {
				i++
			}
			if i >= len(b) {
				return 0, false
			}
			if b[i] == 0x1b {
				return i, true
			}
			return i + 1, true
		}
		return 2, true
	}
}

// stringSeqLen measures an ST-terminated string sequence. An embedded ESC that
// does not begin ST ends the measurement early.
func stringSeqLen(b []byte) (int, bool) {
	for i := 2; i < len(b); i++ {
		switch b[i] {
		case 0x07, 0x9c:
			return i + 1, true
		case 0x1b:
			if i+1 >= len(b) {
				return 0, false
			}
			if b[i+1] == '\\' {
				return i + 2, true
			}
			return i, true
		}
	}
	return 0, false
}

// escapeLenAfterIntro measures a sequence whose introducer has already been
// consumed, without copying the remainder.
func escapeLenAfterIntro(intro byte, rest []byte) (int, bool) {
	switch intro {
	case '[':
		i := 0
		for i < len(rest) && rest[i] >= 0x30 && rest[i] <= 0x3f {
			i++
		}
		for i < len(rest) && rest[i] >= 0x20 && rest[i] <= 0x2f {
			i++
		}
		if i >= len(rest) {
			return 0, false
		}
		if rest[i] == 0x1b {
			return i + 1, true
		}
		return i + 2, true
	case ']', 'P', 'X', '^', '_':
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case 0x07, 0x9c:
				return i + 2, true
			case 0x1b:
				if i+1 >= len(rest) {
					return 0, false
				}
				if rest[i+1] == '\\' {
					return i + 3, true
				}
				return i + 1, true
			}
		}
		return 0, false
	default:
		return 1, true
	}
}

// escapeIsSafe decides whether a complete escape sequence may reach the
// terminal.
func escapeIsSafe(seq []byte) bool {
	if len(seq) < 2 {
		return false
	}
	// A sequence cut short at an ESC is a fragment and must never be emitted.
	if seq[len(seq)-1] == 0x1b {
		return false
	}
	// An 8-bit C1 introducer after ESC: nothing legitimate sends it.
	if seq[1] >= 0x80 {
		return false
	}
	// ESC % <final> selects a character encoding; ESC % @ leaves UTF-8.
	if seq[1] == '%' {
		return false
	}
	if seq[1] == '[' {
		if last := seq[len(seq)-1]; last < 0x40 || last > 0x7e {
			return false
		}
	}
	// ESC <intermediate> <final> is a different family with final range
	// 0x30-0x7e (e.g. ESC ( 0, the box-drawing charset every TUI border uses).
	if seq[1] >= 0x20 && seq[1] <= 0x2f {
		if last := seq[len(seq)-1]; last < 0x30 || last > 0x7e {
			return false
		}
	}
	switch seq[1] {
	case '[':
		return csiIsSafe(seq)
	case ']':
		return oscIsSafe(seq)
	case 'P', 'X', '^', '_':
		return false
	case 'Z':
		// DECID: answers with a device attributes string.
		return false
	case '\\':
		// A string terminator with no string in front of it.
		return false
	}
	return true
}

func csiIsSafe(seq []byte) bool {
	body := seq[2:]
	if len(body) == 0 {
		return false
	}
	final := body[len(body)-1]
	rest := body[:len(body)-1]
	k := len(rest)
	for k > 0 && rest[k-1] >= 0x20 && rest[k-1] <= 0x2f {
		k--
	}
	inter := string(rest[k:])
	params := string(rest[:k])

	switch final {
	case 'c':
		// Device attributes.
		return false
	case 'n':
		// Device status report, including the cursor position report.
		return false
	case 'x':
		// DECREQTPARM asks for the terminal's parameters; DECSACE carries an
		// intermediate byte.
		return inter != ""
	case 'q':
		// CSI > q (XTVERSION) answers with name/version; CSI Ps SP q (DECSCUSR)
		// just picks a cursor shape.
		return !strings.HasPrefix(params, ">")
	case 'p':
		// CSI Ps $ p and CSI ? Ps $ p (DECRQM) answer with a mode's state.
		return inter != "$"
	case 't':
		// Window operations. 22/23 push/pop the title stack (used by TUIs);
		// everything else reports geometry/title or moves the window.
		return params == "22" || params == "23" ||
			strings.HasPrefix(params, "22;") || strings.HasPrefix(params, "23;")
	}
	return true
}

// oscClipboardIsQuery reports whether an OSC 52 payload asks the terminal to
// report the clipboard rather than to set it.
func oscClipboardIsQuery(fields []string) bool {
	if len(fields) < 3 {
		return true
	}
	data := strings.TrimSpace(fields[len(fields)-1])
	return data == "?" || data == ""
}

// oscQueryable are the OSC codes that set colours and accept "?" as a query.
var oscQueryable = map[string]bool{
	"4": true, "5": true, "10": true, "11": true, "12": true, "13": true,
	"14": true, "15": true, "16": true, "17": true, "18": true, "19": true,
	"20": true, "21": true,
}

func oscIsSafe(seq []byte) bool {
	body := seq[2:]
	switch {
	case len(body) >= 1 && (body[len(body)-1] == 0x07 || body[len(body)-1] == 0x9c):
		body = body[:len(body)-1]
	case len(body) >= 2 && body[len(body)-2] == 0x1b && body[len(body)-1] == '\\':
		body = body[:len(body)-2]
	default:
		// Unterminated: a truncated write or an attempt to hide what follows.
		return false
	}

	fields := strings.Split(string(body), ";")
	// Compared as a number, since terminals parse the code numerically.
	code, err := strconv.Atoi(fields[0])
	if err != nil {
		return false
	}
	switch code {
	case 52:
		// Clipboard: a SET is how every TUI copies (allowed); a QUERY reads the
		// host clipboard into the guest's stdin (blocked).
		return !oscClipboardIsQuery(fields)
	case 1337:
		// iTerm2's channel: file writes, clipboard, variable reports.
		return false
	}
	if oscQueryable[strconv.Itoa(code)] {
		for _, f := range fields[1:] {
			if f == "?" {
				return false
			}
		}
	}
	return true
}
