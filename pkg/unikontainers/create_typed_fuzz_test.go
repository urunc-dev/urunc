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
	"slices"
	"sync"
	"testing"

	"github.com/urunc-dev/urunc/internal/fuzzseed"
)

// FuzzCreateTyped drives FuzzCreate's pipeline from typed values, so that every input gets past the
// checks that raw bytes rarely do. sel adds vAccel (bit 3), a Solo5 block and net device (4, 5), a
// queue-proxy container (6) and confinement (7); args, env and mountDests are NUL separated.
func FuzzCreateTyped(f *testing.F) {
	fuzzseed.QuietLogs(f)
	env := newFuzzEnv(f)
	pairs := typedPairs()
	for _, seed := range typedSeeds() {
		fuzzseed.Add(f, seed...)
	}

	f.Fuzz(func(t *testing.T, pair uint8, viaJSON bool, bin, initrd, block, blkMnt, mountRootfs, version,
		args, envList, cwd string, uid, gid uint32, memLimit int64, seccomp bool, mountDests string, sel, host uint8) {
		env.run(t, typedInput(typedSpec{
			pair: pairs[int(pair)%len(pairs)], viaJSON: viaJSON, bin: bin, initrd: initrd, block: block, blkMnt: blkMnt,
			mountRootfs: mountRootfs, version: version, args: args, env: envList, cwd: cwd, uid: uid, gid: gid,
			memLimit: memLimit, seccomp: seccomp, mountDests: mountDests, sel: sel, host: host,
		}))
	})
}

// typedSeeds are the seeds of FuzzCreate that typed values can express, as the arguments of
// FuzzCreateTyped. They are built once: a libFuzzer build calls FuzzCreateTyped for every input.
var typedSeeds = sync.OnceValue(func() [][]any {
	pairs := typedPairs()
	var seeds [][]any
	for _, r := range fuzzseed.Load[seedRow]("FuzzCreate") {
		if r.Guest != "" || r.UruncJSON != "" || r.BadKey != "" { // these only make sense as raw bytes
			continue
		}
		for _, p := range r.pairs() {
			s := r.spec(p)
			seeds = append(seeds, []any{uint8(slices.Index(pairs, p)), s.viaJSON, s.bin, s.initrd, s.block, s.blkMnt,
				s.mountRootfs, s.version, s.args, s.env, s.cwd, s.uid, s.gid, s.memLimit, s.seccomp, s.mountDests, s.sel, s.host})
		}
	}
	return seeds
})
