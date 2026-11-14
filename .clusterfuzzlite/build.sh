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
# Without it a package that fails go test -list would drop its targets silently.
set -o pipefail

# Every fuzz target, as <package dir>:<name>. go test -list skips helpers that are not fuzz targets.
module=$(go list -m)
targets=$(go test -list '^Fuzz' ./pkg/... ./internal/... | awk -v m="$module/" '/^Fuzz/ {name[++n] = $1} /^ok/ {for (i = 1; i <= n; i++) print substr($2, length(m) + 1) ":" name[i]; n = 0}' | sort -u)
test -n "$targets"
# The built fuzzers are named after their function.
test -z "$(cut -d: -f2 <<<"$targets" | sort | uniq -d)"

zips=$(mktemp -d -p "$OUT")
for target in $targets; do
	dir=${target%%:*} fuzzer=${target#*:}
	compile_native_go_fuzzer_v2 "$module/$dir" "$fuzzer" "$fuzzer"

	# compile_native_go_fuzzer_v2 exits 0 when it cannot find the function.
	test -x "$OUT/$fuzzer"

	# libFuzzer ignores f.Add, so the seeds are dumped to files first.
	seeds=$(mktemp -d -p "$zips")
	URUNC_SEED_DIR=$seeds go test -run "^$fuzzer\$" "./$dir"
	if [ -n "$(ls -A "$seeds")" ]; then
		TMPDIR=$zips addStdLibCorpusToFuzzer -dir "$seeds" -fuzzer_name "$fuzzer"
	fi
done
rm -rf "$zips"
