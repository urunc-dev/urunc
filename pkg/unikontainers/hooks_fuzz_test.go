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
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

// hookStub records which hook ran, its argv[0] and the state it got on stdin. The first argument is the
// hook's position in the list; the target adds it, so that the order can be told.
const hookStub = "#!/bin/sh\necho \"$1\" >> \"$FUZZ_HOOK_DIR/order\"\necho \"$0\" > \"$FUZZ_HOOK_DIR/argv0.$1\"\ncat > \"$FUZZ_HOOK_DIR/state.$1\"\n"

// FuzzHooks takes the hooks of a container, as a JSON list of hooks with their args, and
// runs them as urunc does at each point of the container's life. The hooks that run are always
// the stub above, never a path from the input.
func FuzzHooks(f *testing.F) {
	logrus.SetOutput(io.Discard)
	f.Cleanup(func() { logrus.SetOutput(os.Stderr); _ = os.Unsetenv("FUZZ_HOOK_DIR") })
	base := f.TempDir()
	stub, out := filepath.Join(base, "hook.sh"), filepath.Join(base, "out")
	if err := os.WriteFile(stub, []byte(hookStub), 0o755); err != nil { //nolint:gosec
		f.Fatal(err)
	}
	if err := os.Setenv("FUZZ_HOOK_DIR", out); err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var in []struct{ Args []string }
		if json.Unmarshal(data, &in) != nil || len(in) > 4 {
			return
		}
		hooks := make([]specs.Hook, len(in))
		for i, h := range in {
			if slices.ContainsFunc(h.Args, func(a string) bool { return strings.ContainsRune(a, 0) }) { // an engine never writes these
				return
			}
			hooks[i] = specs.Hook{Path: stub, Args: append([]string{"hook-" + strconv.Itoa(i), strconv.Itoa(i)}, h.Args...)}
		}
		u := &Unikontainer{
			State: &specs.State{Version: specs.Version, ID: "fuzz", Status: specs.StateCreating},
			Spec: &specs.Spec{Hooks: &specs.Hooks{CreateRuntime: hooks, CreateContainer: hooks, StartContainer: hooks,
				Poststart: hooks, Poststop: hooks}},
		}
		for _, name := range []string{"CreateRuntime", "CreateContainer", "StartContainer", "Poststart", "Poststop"} {
			if err := os.RemoveAll(out); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := u.ExecuteHooks(name); err != nil {
				t.Fatalf("%s hooks failed: %v", name, err)
			}
			checkHooks(t, hookRun{name, len(hooks), out})
		}
	})
}

// hookRun is what the hooks of one point in the container's life left behind.
type hookRun struct {
	name string
	n    int    // how many hooks there were
	out  string // where the stub recorded what it saw
}

// hookChecks are strict checks on hooks, registered from the files that hold them.
var hookChecks []func(t *testing.T, h hookRun)

// checkHooks fails if a hook did not run once, did not get the container's state on stdin, or,
// with the check "hooks-order" on, did not run in the order of the list.
func checkHooks(t *testing.T, h hookRun) {
	order, _ := os.ReadFile(filepath.Join(h.out, "order"))
	ran := strings.Fields(string(order))
	if len(ran) != h.n {
		t.Fatalf("%s: %d hooks ran, the container has %d", h.name, len(ran), h.n)
	}
	for i := range h.n {
		var state specs.State
		data, _ := os.ReadFile(filepath.Join(h.out, "state."+strconv.Itoa(i)))
		if json.Unmarshal(bytes.TrimSpace(data), &state) != nil || state.ID != "fuzz" {
			t.Fatalf("%s: hook %d got %q on stdin, not the container's state", h.name, i, data)
		}
	}
	if strictOn("hooks-order") {
		for i, got := range ran {
			if got != strconv.Itoa(i) {
				t.Fatalf("%s: hooks ran in the order %v, the list is in order 0 to %d", h.name, ran, h.n-1)
			}
		}
	}
	for _, check := range hookChecks {
		check(t, h)
	}
}
