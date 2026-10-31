#!/bin/bash -eu

# Copyright (c) 2023-2026, Nubificus LTD
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Only meant to run inside ClusterFuzzLite or OSS-Fuzz, which provide
# compile_native_go_fuzzer_v2.

# go-118-fuzz-build cannot patch a toolchain that go downloads into the module cache.
export GOTOOLCHAIN=local

# addStdLibCorpusToFuzzer makes each zip in TMPDIR and moves it to $OUT, which can be another filesystem.
zips=$(mktemp -d -p "$OUT")
# Every fuzz target of the repo, as dir:Name.
targets=$(grep -rHoE --include='*_test.go' '^func Fuzz[A-Za-z0-9_]+' pkg internal | sed 's|/[^/]*:func |:|' | sort -u)
test -n "$targets"
# The binaries and zips are named after the target, so a name cannot be used twice.
test -z "$(cut -d: -f2 <<<"$targets" | sort | uniq -d)"
module=$(go list -m)
for t in $targets; do
	dir=${t%%:*} fuzzer=${t#*:}
	compile_native_go_fuzzer_v2 "$module/$dir" "$fuzzer" "$fuzzer"
	# compile_native_go_fuzzer_v2 exits 0 when it cannot find the function.
	test -x "$OUT/$fuzzer"
	# libFuzzer ignores f.Add, so the seeds are dumped to files first.
	seeds=$(mktemp -d)
	URUNC_SEED_DIR=$seeds go test -run "^$fuzzer\$" "./$dir"
	TMPDIR=$zips addStdLibCorpusToFuzzer -dir "$seeds" -fuzzer_name "$fuzzer"
done
rmdir "$zips"
