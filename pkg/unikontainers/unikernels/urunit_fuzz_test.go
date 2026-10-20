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
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// urunitView is what urunit applies from its config: env, uid, gid and cwd.
type urunitView struct {
	env      []string
	uid, gid uint32
	wd       string
	argv     []string // ARC/ARV, FreeBSD only
}

type urunitTok struct {
	pos int
	s   string
}

// urunitStrtok splits like strtok(s, "\n"): empty lines vanish.
func urunitStrtok(s string) []urunitTok {
	var toks []urunitTok
	for i := 0; i < len(s); {
		j := strings.IndexByte(s[i:], '\n')
		if j < 0 {
			j = len(s) - i
		}
		if j > 0 {
			toks = append(toks, urunitTok{i, s[i : i+j]})
		}
		i += j + 1
	}
	return toks
}

// urunitParseConf models urunit's get_env_vars, parse_envs and parse_process_config
// (nubificus/urunit src/main.c): markers match by prefix, later fields overwrite.
func urunitParseConf(conf string) (urunitView, bool) {
	v := urunitView{wd: "/"} // no process config: urunit's own uid 0 and cwd /
	rest := conf
	if strings.HasPrefix(rest, envStartMarker) {
		end := -1
		for _, tk := range urunitStrtok(rest)[1:] {
			if strings.HasPrefix(tk.s, envEndMarker) {
				end = min(tk.pos+4, len(rest))
				break
			}
			v.env = append(v.env, tk.s)
		}
		if end < 0 {
			return urunitView{}, false
		}
		rest = rest[end:]
	}
	if strings.HasPrefix(rest, lpcStartMarker) {
		argc := 0
		for _, tk := range urunitStrtok(rest)[1:] {
			_, val, hasColon := strings.Cut(tk.s, ":")
			switch {
			case strings.HasPrefix(tk.s, "UID:"), strings.HasPrefix(tk.s, "GID"):
				// get_uint_val: strtoul on the text after the first ':' (sign not modelled)
				if !hasColon || val == "" {
					return urunitView{}, false
				}
				val = strings.TrimLeft(val, " \t\n\v\f\r")
				u, err := strconv.ParseUint("0"+val[:len(val)-len(strings.TrimLeft(val, "0123456789"))], 10, 32)
				if err != nil {
					return urunitView{}, false
				}
				if strings.HasPrefix(tk.s, "UID:") {
					v.uid = uint32(u)
				} else {
					v.gid = uint32(u)
				}
			case strings.HasPrefix(tk.s, "WD"):
				val = strings.TrimLeft(val, " \t\n\v\f\r")
				if !hasColon || val == "" {
					return urunitView{}, false
				}
				v.wd = val
			case strings.HasPrefix(tk.s, "ARC:"):
				n, err := strconv.ParseUint(strings.TrimLeft(val, " \t\n\v\f\r"), 10, 32)
				if err != nil || v.argv != nil {
					return urunitView{}, false
				}
				v.argv, argc = []string{}, int(n)
			case strings.HasPrefix(tk.s, "ARV:"):
				if v.argv == nil || len(v.argv) >= argc {
					return urunitView{}, false
				}
				v.argv = append(v.argv, tk.s[4:])
			case strings.HasPrefix(tk.s, lpcEndMarker):
				if v.argv != nil && len(v.argv) != argc {
					return urunitView{}, false
				}
				return v, true
			}
		}
		return urunitView{}, false
	}
	return v, true
}

// FuzzBuildUrunitConfig checks urunit applies exactly the env, user and cwd
// urunc wrote, using a model of urunit's own config parser.
func FuzzBuildUrunitConfig(f *testing.F) {
	f.Add("PATH=/usr/bin", "HOME=/root", "/work", uint32(0), uint32(0), "blk0", "/data", "qemu")
	f.Add("A=1", "B=2", "/", uint32(1000), uint32(1000), "id", "/mnt", "firecracker")
	f.Add("X=1\nUEE\nUCS\nUID:0\nGID:0\nWD:/\nUCE", "B=2", "/tmp", uint32(1000), uint32(1000), "", "", "qemu")
	f.Add("UEE_TOKEN=x", "B=2", "/tmp", uint32(1000), uint32(1000), "", "", "qemu")

	f.Fuzz(func(t *testing.T, env1, env2, workDir string, uid, gid uint32,
		blkID, blkMP, monitor string) {
		if !strictOn("urunit-config") {
			return
		}
		for _, e := range []string{env1, env2} {
			if name, _, ok := strings.Cut(e, "="); !ok || name == "" || strings.TrimSpace(name) != name {
				return // engines always hand over NAME=value with a plain name
			}
		}
		if !strings.HasPrefix(workDir, "/") {
			return // OCI requires an absolute cwd
		}
		l := &Linux{
			Env:        []string{env1, env2},
			Monitor:    monitor,
			ProcConfig: types.ProcessConfig{UID: uid, GID: gid, WorkDir: workDir},
			Blk:        []types.BlockDevParams{{ID: blkID, MountPoint: blkMP}},
		}
		out := l.buildUrunitConfig()
		got, ok := urunitParseConf(out)
		want := urunitView{env: l.Env, uid: uid, gid: gid, wd: workDir}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("urunit applies a different process config than urunc wrote (parsed=%v)\n  want: %+v\n  got:  %+v\n  conf: %q",
				ok, want, got, out)
		}
	})
}

// FuzzParseCmdLine: parseCmdLine quotes args with spaces but never escapes a
// quote already present, so the emitted command must stay balanced.
func FuzzParseCmdLine(f *testing.F) {
	f.Add("/bin/sh", "-c", "echo hi")
	f.Add("app", "arg with space", "plain")
	f.Add("app", "", "")

	f.Fuzz(func(t *testing.T, a0, a1, a2 string) {
		if !strictOn("urunit-parse") {
			return
		}
		l := newLinux()
		if err := l.parseCmdLine([]string{a0, a1, a2}); err != nil {
			return
		}
		joined := l.App + " " + l.Command
		if n := strings.Count(joined, "'"); n%2 != 0 {
			// parseCmdLine quotes args containing a space but never escapes a
			// quote already in the arg itself.
			t.Fatalf("parseCmdLine(%q) produced %q, which contains %d single quote(s) -- unbalanced, so urunit's re-split of this command line does not recover the original arguments",
				[]string{a0, a1, a2}, joined, n)
		}
	})
}

// TestUrunitConfModel pins the model to real boots (image /urunit, 6.12 kernel).
func TestUrunitConfModel(t *testing.T) {
	for _, tc := range []struct {
		env  []string
		wd   string
		want urunitView
	}{
		{[]string{"A=1", "B=2"}, "/tmp", urunitView{[]string{"A=1", "B=2"}, 1000, 1000, "/tmp", nil}},
		{[]string{"A=1", "X=1\nUEE\nUCS\nUID:0\nGID:0\nWD:/\nUCE", "B=2"}, "/tmp", urunitView{[]string{"A=1", "X=1"}, 0, 0, "/", nil}},
		{[]string{"A=1"}, "/tmp\nUID:0\nGID:0", urunitView{[]string{"A=1"}, 0, 0, "/tmp", nil}},
		{[]string{"A=1", "UEE_TOKEN=x", "B=2"}, "/tmp", urunitView{[]string{"A=1"}, 0, 0, "/", nil}},
		{[]string{"A=1", "NOTE=line one\nline two", "B=2"}, "/tmp", urunitView{[]string{"A=1", "NOTE=line one", "line two", "B=2"}, 1000, 1000, "/tmp", nil}},
	} {
		l := &Linux{Env: tc.env, ProcConfig: types.ProcessConfig{UID: 1000, GID: 1000, WorkDir: tc.wd}}
		if got, _ := urunitParseConf(l.buildUrunitConfig()); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("env %q wd %q: got %+v, want %+v", tc.env, tc.wd, got, tc.want)
		}
	}
}

// FuzzBuildFreeBSDUrunitConfig: on FreeBSD the app argv also travels in the urunit
// config (ARC/ARV), so args, env, user and cwd must all come back intact.
func FuzzBuildFreeBSDUrunitConfig(f *testing.F) {
	f.Add("/probe", "a b", "A=1", "/tmp", uint32(1000), uint32(1000))
	f.Add("/probe", "x\nUID:0\nGID:0", "A=1", "/tmp", uint32(1000), uint32(1000))

	f.Fuzz(func(t *testing.T, a0, a1, env1, workDir string, uid, gid uint32) {
		if !strictOn("urunit-freebsd") {
			return
		}
		if name, _, ok := strings.Cut(env1, "="); !ok || name == "" || strings.TrimSpace(name) != name {
			return // engines always hand over NAME=value with a plain name
		}
		if !strings.HasPrefix(workDir, "/") || a0 == "" {
			return // OCI requires an absolute cwd; the app path is never empty
		}
		fb := &FreeBSD{Env: []string{env1}, Command: []string{a0, a1}, Monitor: "qemu",
			ProcConfig: types.ProcessConfig{UID: uid, GID: gid, WorkDir: workDir}}
		out := fb.buildUrunitConfig()
		got, ok := urunitParseConf(out)
		want := urunitView{env: fb.Env, uid: uid, gid: gid, wd: workDir, argv: fb.Command}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("urunit applies a different process config than urunc wrote (parsed=%v)\n  want: %+v\n  got:  %+v\n  conf: %q",
				ok, want, got, strings.TrimRight(out, "\n"))
		}
	})
}

// TestFreeBSDUrunitConfModel pins the model to FreeBSD+urunit boots (qemu microvm, 9p root).
func TestFreeBSDUrunitConfModel(t *testing.T) {
	for _, tc := range []struct {
		args, env []string
		wd        string
		want      urunitView
		ok        bool
	}{
		{[]string{"/probe", "a b"}, []string{"A=1"}, "/tmp", urunitView{[]string{"A=1"}, 1000, 1000, "/tmp", []string{"/probe", "a b"}}, true},
		{[]string{"/probe", "x\nUID:0\nGID:0"}, []string{"A=1"}, "/tmp", urunitView{[]string{"A=1"}, 0, 0, "/tmp", []string{"/probe", "x"}}, true},
		{[]string{"/probe"}, []string{"A=1"}, "/tmp\nUID:0\nGID:0", urunitView{[]string{"A=1"}, 0, 0, "/tmp", []string{"/probe"}}, true},
		{[]string{"/probe", "line1\nline2"}, nil, "/tmp", urunitView{nil, 1000, 1000, "/tmp", []string{"/probe", "line1"}}, true},
		// No process section parsed -> no argv -> "Going nowhere without my init!" panic.
		{[]string{"/probe"}, []string{"UEE_TOKEN=x", "B=2"}, "/tmp", urunitView{nil, 0, 0, "/", nil}, true},
	} {
		fb := &FreeBSD{Env: tc.env, Command: tc.args, ProcConfig: types.ProcessConfig{UID: 1000, GID: 1000, WorkDir: tc.wd}}
		if got, ok := urunitParseConf(fb.buildUrunitConfig()); ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("args %q env %q wd %q: got %+v ok=%v, want %+v", tc.args, tc.env, tc.wd, got, ok, tc.want)
		}
	}
}
