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

// Models of how each guest parses what urunc hands it, used as FuzzCreatePipeline's
// output oracles. Every model is pinned by a Test*Model built from real boots.

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// Kernel command-line model, ported from lib/cmdline.c next_arg() and
// kernel/params.c parse_args() (torvalds/linux master, fetched 2026-09-26).
func kernelIsSpace(c byte) bool { return c == ' ' || (c >= 9 && c <= 13) || c == 0xa0 }

func kernelSkipSpaces(b []byte) []byte {
	for len(b) > 0 && kernelIsSpace(b[0]) {
		b = b[1:]
	}
	return b
}

// kernelNextArg returns one token as the kernel hands it on ("param" or
// "param=val", after repair_env_string), whether it had a value, and the rest.
func kernelNextArg(args []byte) (tok string, hasVal bool, rest []byte) {
	quoted := len(args) > 0 && args[0] == '"'
	if quoted {
		args = args[1:]
	}
	inQuote := quoted
	i, equals := 0, 0
	for ; i < len(args); i++ {
		if kernelIsSpace(args[i]) && !inQuote {
			break
		}
		if equals == 0 && args[i] == '=' {
			equals = i
		}
		if args[i] == '"' {
			inQuote = !inQuote
		}
	}
	buf := append([]byte(nil), args[:i]...)
	cstr := func(b []byte) string {
		if n := bytes.IndexByte(b, 0); n >= 0 {
			b = b[:n]
		}
		return string(b)
	}
	if equals == 0 {
		if quoted && i > 0 && buf[i-1] == '"' {
			buf[i-1] = 0
		}
		tok = cstr(buf)
	} else {
		buf[equals] = 0
		valStart := equals + 1
		if valStart < i && buf[valStart] == '"' {
			valStart++
			if buf[i-1] == '"' {
				buf[i-1] = 0
			}
		}
		if quoted && i > 0 && buf[i-1] == '"' {
			buf[i-1] = 0
		}
		val := ""
		if valStart <= i {
			val = cstr(buf[valStart:])
		}
		tok, hasVal = cstr(buf)+"="+val, true
	}
	if i < len(args) {
		i++
	}
	return tok, hasVal, kernelSkipSpaces(args[i:])
}

// kernelParse splits a command line the way the kernel does: tokens before
// "--", then init's argv, which a second "--" cuts short (cut=true).
func kernelParse(cmdline string) (boot, initArgs []string, sawDashes, cut bool) {
	pass := func(b []byte) ([]string, []byte, bool) {
		var toks []string
		b = kernelSkipSpaces(b)
		for len(b) > 0 {
			var tok string
			var hasVal bool
			tok, hasVal, b = kernelNextArg(b)
			if !hasVal && tok == "--" {
				return toks, b, true
			}
			toks = append(toks, tok)
		}
		return toks, nil, false
	}
	boot, rest, sawDashes := pass([]byte(cmdline))
	if sawDashes {
		initArgs, _, cut = pass(rest)
	}
	return boot, initArgs, sawDashes, cut
}

// kernelInitArgLimit is CONFIG_INIT_ENV_ARG_LIMIT's default; past it the
// kernel panics ("Too many boot init/env vars").
const kernelInitArgLimit = 32

const linuxCmdlineMax = 2047

// unikraftMaxTokens: urunc's hello-env Unikraft image boots with 58 cmdline
// tokens and crashes at 59 (measured under KVM, 2026-09-26).
const unikraftMaxTokens = 58

// ukArgParse ports uk_argnparse (unikraft lib/ukargparse/argparse.c, staging
// 2026-09-26); quote and backslash handling matched a real boot.
func ukArgParse(s string) []string {
	var toks []string
	var cur []byte
	inTok, esc := false, false
	var quote byte
	for i := 0; i < len(s) && s[i] != 0; i++ {
		c := s[i]
		if (c == ' ' || c == '\r' || c == '\n' || c == '\t' || c == '\v') && quote == 0 && !esc {
			if inTok {
				toks, cur, inTok = append(toks, string(cur)), nil, false
			}
			continue
		}
		if c == '\'' || c == '"' {
			switch {
			case esc && quote == 0: // escaped quote is a plain char
			case esc && c == '"': // \" inside quotes -> "
				cur, esc = cur[:len(cur)-1], false
			case quote == 0:
				quote = c
				continue
			case quote == c:
				quote = 0
				continue
			}
		}
		if !inTok {
			inTok, esc = true, false
		}
		cur = append(cur, c)
		if c == '\\' && quote != '\'' {
			if esc {
				cur, esc = cur[:len(cur)-1], false
			} else {
				esc = true
			}
		} else if esc {
			if quote == 0 {
				cur = append(cur[:len(cur)-2], c)
			}
			esc = false
		}
	}
	if inTok {
		toks = append(toks, string(cur))
	}
	return toks
}

// ukLibparamList returns the values uklibparam's parse_arg (lib/uklibparam/
// parser.c) collects for list param name, and the app args after the first "--".
func ukLibparamList(toks []string, name string) (vals, appArgs []string) {
	inList, mine := false, false
	for i, tok := range toks {
		v := tok
		if !inList {
			if tok == "--" {
				return vals, toks[i+1:]
			}
			_, afterDot, _ := strings.Cut(tok, ".") // "lib.param" needs the dot first
			_, val, hasVal := strings.Cut(afterDot, "=")
			if !hasVal || !strings.HasPrefix(val, "[") {
				continue
			}
			inList, mine, v = true, strings.HasPrefix(tok, name+"=["), val[1:]
		}
		if strings.HasSuffix(v, "]") {
			inList, v = false, v[:len(v)-1]
		}
		if mine && v != "" {
			vals = append(vals, v)
		}
	}
	return vals, nil
}

// rumprunView models rumprun's config.c: jsmn hands each string token on raw
// (no JSON unescaping); env goes to putenv, cmdline to rumprun_parseargs.
func rumprunView(config string) (argv, env []string) {
	dec := json.NewDecoder(strings.NewReader(config))
	if _, err := dec.Token(); err != nil {
		return nil, nil
	}
	for dec.More() {
		key, err := dec.Token()
		var raw json.RawMessage
		if err != nil || dec.Decode(&raw) != nil {
			return nil, nil
		}
		if len(raw) < 2 || raw[0] != '"' {
			continue
		}
		switch tok := string(raw[1 : len(raw)-1]); key {
		case "cmdline":
			argv = rumprunParseArgs(tok)
		case "env":
			env = append(env, tok)
		}
	}
	return argv, env
}

// rumprunParseArgs ports rumprun_parseargs (lib/librumprun_base/parseargs.c).
func rumprunParseArgs(s string) []string {
	var args []string
	var out []byte
	quote := -1 // -1 outside an arg, 0 inside, or the open quote char
	for i := 0; ; {
		var ac, rc byte
		if i < len(s) {
			ac = s[i]
		}
		i++
		rc = ac
		if ac == '\\' {
			rc = 0
			if i < len(s) {
				rc = s[i]
			}
			i++
		}
		if rc == 0 || ac == ' ' || ac == '\n' || ac == '\t' {
			if rc == 0 || quote == 0 {
				if quote >= 0 {
					args = append(args, string(out))
				}
				out, quote = nil, -1
			}
			if rc == 0 {
				return args
			}
			if quote < 0 {
				continue
			}
		}
		if quote < 0 {
			quote = 0
		}
		if quote > 0 && int(ac) == quote {
			quote = 0
			continue
		}
		if ac == '\'' || ac == '"' {
			quote = int(ac)
			continue
		}
		out = append(out, rc)
	}
}

// hermitShellWords ports shell_words::split (the crate the Hermit kernel uses for
// bootargs): POSIX quotes and backslashes, '#' starts a comment; false = parse error.
//
//nolint:gocyclo // one flat case list
func hermitShellWords(s string) ([]string, bool) {
	const (
		delim = iota
		backslash
		unquoted
		unquotedBackslash
		single
		double
		doubleBackslash
		comment
	)
	var words []string
	var word []rune
	state := delim
	for _, c := range s + "\x00" {
		end := c == 0
		switch state {
		case delim:
			switch {
			case end:
			case c == '\'':
				state = single
			case c == '"':
				state = double
			case c == '\\':
				state = backslash
			case c == ' ' || c == '\t' || c == '\n':
			case c == '#':
				state = comment
			default:
				word, state = append(word, c), unquoted
			}
		case backslash, unquotedBackslash:
			switch {
			case end:
				words = append(words, string(append(word, '\\')))
			case c == '\n' && state == backslash:
				state = delim
			case c == '\n':
				state = unquoted
			default:
				word, state = append(word, c), unquoted
			}
		case unquoted:
			switch {
			case end:
				words = append(words, string(word))
			case c == '\'':
				state = single
			case c == '"':
				state = double
			case c == '\\':
				state = unquotedBackslash
			case c == ' ' || c == '\t' || c == '\n':
				words, word, state = append(words, string(word)), nil, delim
			default:
				word = append(word, c)
			}
		case single:
			switch {
			case end:
				return nil, false
			case c == '\'':
				state = unquoted
			default:
				word = append(word, c)
			}
		case double:
			switch {
			case end:
				return nil, false
			case c == '"':
				state = unquoted
			case c == '\\':
				state = doubleBackslash
			default:
				word = append(word, c)
			}
		case doubleBackslash:
			switch {
			case end:
				return nil, false
			case c == '\n':
			case strings.ContainsRune("$`\"\\", c):
				word = append(word, c)
			default:
				word = append(word, '\\', c)
			}
			if !end {
				state = double
			}
		case comment:
			if c == '\n' {
				state = delim
			}
		}
	}
	return words, true
}

// urunitSpawnArgs ports urunit's spawn_app (nubificus/urunit src/main.c): an arg that
// opens a ' or " without closing it is glued to later args up to one ending in that
// quote, into a 1024-byte buffer, and every arg after that run is dropped.
func urunitSpawnArgs(args []string) (argv []string, overflow bool) {
	for i, a := range args {
		if a == "" || a[0] != '\'' && a[0] != '"' || a[len(a)-1] == a[0] {
			argv = append(argv, a)
			continue
		}
		q, buf := a[0], a[1:]
		for _, n := range args[i+1:] {
			if n == "" {
				continue
			}
			stop := false
			if n[len(n)-1] == q && (len(n) == 1 || n[len(n)-2] != q) {
				stop, n = true, n[:len(n)-1]
			}
			buf += " " + n
			if stop {
				break
			}
		}
		return append(argv, buf), len(buf) >= 1024
	}
	return argv, false
}

// TestUnikraftCmdlineModel pins the model to what the real guest did under KVM.
func TestUnikraftCmdlineModel(t *testing.T) {
	fstab := ` vfs.fstab=[ "initrd0:/:extract:::" ] -- /helloworld`
	for _, tc := range []struct {
		env  string
		want []string
	}{
		{"URUNC=hi", []string{"URUNC=hi"}},
		{"URUNC=hello world", []string{"URUNC=hello", "world"}},
		{`URUNC=a\b`, []string{"URUNC=ab"}},
		{"URUNC=it''s", []string{"URUNC=its"}},
		{"Q='x y z w'", []string{"Q=x y z w"}},
		{"URUNC=a]b", []string{"URUNC=a]b"}},
		{"URUNC=] LANG=after", []string{"URUNC="}},
		{"URUNC=a] env.vars=[LANG=injected]", []string{"URUNC=a", "LANG=injected"}},
	} {
		if got, _ := ukLibparamList(ukArgParse("Unikraft  env.vars=[ "+tc.env+" ]"+fstab), "env.vars"); !slices.Equal(got, tc.want) {
			t.Errorf("env %q: got %q, want %q", tc.env, got, tc.want)
		}
	}
	if n := len(ukArgParse("Unikraft  env.vars=[ Q='x y z w' ]" + fstab + strings.Repeat(" a", 49))); n != 58 {
		t.Errorf("booting 58-token cmdline counted as %d", n)
	}
}

// TestRumprunConfigModel pins the model to real hvt boots of a Rumprun probe.
func TestRumprunConfigModel(t *testing.T) {
	for _, tc := range []struct {
		config    string
		argv, env []string
	}{
		{`{"cmdline":"probe a","env":"X=1"}`, []string{"probe", "a"}, []string{"X=1"}},
		{`{"cmdline":"probe","env":"URL=http://x/?a=1\u0026b=2"}`, []string{"probe"}, []string{`URL=http://x/?a=1\u0026b=2`}},
		{`{"cmdline":"probe hello world x"}`, []string{"probe", "hello", "world", "x"}, nil},
		{`{"cmdline":"probe a\u0026b \u003ctag\u003e"}`, []string{"probe", "au0026b", "u003ctagu003e"}, nil},
		{`{"cmdline":"probe","env":"MSG=say \"hi\""}`, []string{"probe"}, []string{`MSG=say \"hi\"`}},
		{`{"cmdline":"probe say\"hi"}`, []string{"probe", `say"hi`}, nil},
		{`{"cmdline":"probe","env":"WIN=C:\\\\dir"}`, []string{"probe"}, []string{`WIN=C:\\\\dir`}},
		{`{"cmdline":"probe C:\\\\dir"}`, []string{"probe", `C:\\dir`}, nil},
		{`{"cmdline":"probe ","env":"E=a\nb"}`, []string{"probe"}, []string{`E=a\nb`}},
		{`{"cmdline":"probe it's x"}`, []string{"probe", "its x"}, nil},
	} {
		argv, env := rumprunView(tc.config)
		if !slices.Equal(argv, tc.argv) || !slices.Equal(env, tc.env) {
			t.Errorf("%s: got argv %q env %q, want %q %q", tc.config, argv, env, tc.argv, tc.env)
		}
	}
}

// TestUnikraftArgsModel pins app args to real boots of urunc's args-unikraft-qemu image.
func TestUnikraftArgsModel(t *testing.T) {
	for _, tc := range []struct {
		args, want []string
	}{
		{[]string{"/f", "a b"}, []string{"/f", "a", "b"}},
		{[]string{"/f", "it's"}, []string{"/f", "its"}},
		{[]string{"/f", `say"hi`}, []string{"/f", "sayhi"}},
		{[]string{"/f", `a\b`}, []string{"/f", "ab"}},
		{[]string{"/f", ""}, []string{"/f"}},
		{[]string{"/f", "x", "--", "y"}, []string{"/f", "x", "--", "y"}},
		{[]string{"/f", "a\tb"}, []string{"/f", "a", "b"}},
		{[]string{"/f", "é", "a]"}, []string{"/f", "é", "a]"}},
	} {
		cmd := "Unikraft   netdev.ip=/24::8.8.8.8    -- " + strings.Join(tc.args, " ")
		if _, got := ukLibparamList(ukArgParse(cmd), "env.vars"); !slices.Equal(got, tc.want) {
			t.Errorf("args %q: got %q, want %q", tc.args, got, tc.want)
		}
	}
}

// TestHermitArgsModel pins the model to boots of a Hermit argv probe (hermit 0.11 kernel).
func TestHermitArgsModel(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want []string
		ok   bool
	}{
		{"probe a b", nil, true}, // no "--": urunc's no-network form, app gets nothing
		{"ip=10.0.0.2/24 gateway=10.0.0.1 -- probe a b", []string{"probe", "a", "b"}, true},
		{"x -- probe 'a b' a\\b '' a\\ b", []string{"probe", "a b", "ab", "", "a b"}, true},
		{"x -- probe it's", nil, false},
		{`x -- probe say"hi`, nil, false},
		{"x -- probe x -- y", []string{"probe", "x", "--", "y"}, true},
		{"x -- probe #tag x", []string{"probe"}, true},
		{`x -- probe a#b "a\"b" "c\d"`, []string{"probe", "a#b", `a"b`, `c\d`}, true},
	} {
		words, ok := hermitShellWords(tc.cmd)
		var got []string
		for i, w := range words {
			if w == "--" {
				got = words[i+1:]
				break
			}
		}
		if ok != tc.ok || ok && !slices.Equal(got, tc.want) {
			t.Errorf("%q: got %q ok=%v, want %q ok=%v", tc.cmd, got, ok, tc.want, tc.ok)
		}
	}
}

// TestUrunitSpawnModel pins the spawn_app port to boots of the image's /urunit.
func TestUrunitSpawnModel(t *testing.T) {
	for _, tc := range []struct {
		tail string
		want []string
	}{
		{"/probe 'a b' c", []string{"/probe", "a b"}},
		{"/probe 'x' y", []string{"/probe", "'x'", "y"}},
		{"/probe 'abc d", []string{"/probe", "abc d"}},
		{"/probe a 'b c' 'd e'", []string{"/probe", "a", "b c"}},
	} {
		_, initArgs, _, _ := kernelParse("panic=-1 console=ttyS0 root=/dev/ram0 rw rdinit=/urunit -- " + tc.tail)
		if got, _ := urunitSpawnArgs(initArgs); !slices.Equal(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.tail, got, tc.want)
		}
	}
}
