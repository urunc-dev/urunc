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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	fuzzheaders "github.com/AdaLogics/go-fuzz-headers"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// state.json is a trust boundary reused by every later command: create writes it,
// every later command reconstructs from it. Uses the REAL
// saveContainerState/loadUnikontainerState, not a mirror, so it's slower but
// tests the actual write-then-read cycle.
//
//nolint:gocyclo // one flat case list
func FuzzStatePersistenceRoundTrip(f *testing.F) {
	f.Add([]byte("urunc-state-seed"))
	f.Add([]byte("com.urunc.unikernel.binary"))

	f.Fuzz(func(t *testing.T, data []byte) {
		c := fuzzheaders.NewConsumer(data)

		specAnnots := map[string]string{}
		stateAnnots := map[string]string{}
		if err := c.FuzzMap(&specAnnots); err != nil {
			return
		}
		if err := c.FuzzMap(&stateAnnots); err != nil {
			return
		}

		// Force overlap between the maps: without it the "state value wins"
		// merge check below is dead code (mutation testing caught this), and
		// overlap is the realistic case anyway.
		for k, v := range specAnnots {
			if _, exists := stateAnnots[k]; !exists {
				// A distinct value, so "which one won" is observable.
				stateAnnots[k] = v + "-state"
			}
			break // one shared key is enough to exercise the branch
		}

		// json.Marshal replaces invalid UTF-8 with U+FFFD (stdlib behavior) --
		// filtered here so it doesn't bury the properties below.
		for k, v := range specAnnots {
			if !utf8.ValidString(k) || !utf8.ValidString(v) {
				return
			}
		}
		for k, v := range stateAnnots {
			if !utf8.ValidString(k) || !utf8.ValidString(v) {
				return
			}
		}
		// NOT t.TempDir(): its cleanup only runs at the end of the whole fuzz
		// run, so dirs piled up and the exec rate decayed to 0 within ~3
		// minutes when tried. MkdirTemp + explicit RemoveAll keeps it flat.
		baseDir, err := os.MkdirTemp("", "urunc-state-fuzz-")
		if err != nil {
			t.Skipf("harness setup failed: %v", err)
		}
		defer os.RemoveAll(baseDir)

		// saveContainerState mutates u.State.Annotations in place as part of
		// the merge, so snapshot the caller's pre-save view first.
		preState := make(map[string]string, len(stateAnnots))
		for k, v := range stateAnnots {
			preState[k] = v
		}

		u := &Unikontainer{
			BaseDir: baseDir,
			State: &specs.State{
				Version:     "1.0.2",
				ID:          "fuzz-container",
				Status:      "created",
				Bundle:      "/bundle",
				Annotations: stateAnnots,
			},
			Spec: &specs.Spec{
				Annotations: specAnnots,
			},
		}

		if err := u.saveContainerState(); err != nil {
			return // host or marshal failure, not the property under test
		}

		reloaded, err := loadUnikontainerState(filepath.Join(baseDir, stateFilename))
		if err != nil {
			t.Fatalf("state.json written by saveContainerState could not be read back by "+
				"loadUnikontainerState: %v", err)
		}
		if reloaded.Annotations == nil && len(preState) > 0 {
			t.Fatalf("state.json round trip produced nil annotations from a state that had %d",
				len(preState))
		}

		// P1: saveContainerState copies spec annotations into state only where
		// the key is absent, so a key in both must keep the state value, and
		// every spec key must end up present.
		for k, specVal := range specAnnots {
			got, ok := reloaded.Annotations[k]
			if !ok {
				t.Fatalf("annotation %q was in Spec.Annotations but is missing from the "+
					"reloaded state; saveContainerState propagates all spec annotations "+
					"into state on purpose", k)
			}
			if stateVal, inBoth := preState[k]; inBoth {
				if got != stateVal {
					t.Fatalf("annotation %q is in BOTH Spec and State: the state value %q must "+
						"win, but the reloaded state has %q (spec value was %q)",
						k, stateVal, got, specVal)
				}
			} else if got != specVal {
				t.Fatalf("annotation %q came only from Spec with value %q but reloaded as %q",
					k, specVal, got)
			}
		}

		// P2: nothing the state already carried may be lost.
		for k, want := range preState {
			got, ok := reloaded.Annotations[k]
			if !ok {
				t.Fatalf("annotation %q was in State.Annotations before save but is absent "+
					"after the state.json round trip", k)
			}
			if got != want {
				t.Fatalf("annotation %q changed across the state.json round trip:\n"+
					"  before: %q\n  after:  %q", k, want, got)
			}
		}

		// P4: the config urunc rebuilds from reloaded state must match. Dotted
		// monitor names are skipped: the flat config map cannot carry them.
		for k := range reloaded.Annotations {
			if strings.HasPrefix(k, "urunc_config.monitors.") &&
				len(strings.Split(k, ".")) != 4 {
				return
			}
		}

		fromReloaded := UruncConfigFromMap(reloaded.Annotations)
		fromDirect := UruncConfigFromMap(preState)
		if fromReloaded == nil || fromDirect == nil {
			return
		}
		for name, want := range fromDirect.Monitors {
			got, ok := fromReloaded.Monitors[name]
			if !ok {
				t.Fatalf("monitor %q survives UruncConfigFromMap when read straight from the "+
					"in-memory map, but is gone once the same annotations make a real "+
					"state.json round trip", name)
			}
			if got != want {
				t.Fatalf("monitor %q differs depending on whether the annotations went through "+
					"a real state.json round trip:\n  direct:   %+v\n  via file: %+v",
					name, want, got)
			}
		}
	})
}
