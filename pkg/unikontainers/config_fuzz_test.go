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
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

// b64 is a helper for building seed corpus entries that are valid base64.
func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// fuzzBundle creates a bundle directory once per fuzz worker process and
// returns the bundle dir, the path of the urunc.json inside its rootfs, and a
// spec pointing at that rootfs.
//
// This is deliberately hoisted out of the fuzz function. Creating the bundle
// per iteration with t.TempDir() costs a mkdir plus a recursive cleanup on
// every execution and drops throughput by roughly two orders of magnitude,
// which is enough to stop the fuzzer finding shallow bugs in a CI-length run.
// Reusing one directory and overwriting the file is safe here because each
// worker process gets its own f.TempDir() and executes iterations serially.
func fuzzBundle(f *testing.F) (string, string, *specs.Spec) {
	f.Helper()

	// Workload-controlled input makes getConfigFromJSON emit an Error-level
	// log line per field that fails to base64-decode. Left enabled, that
	// serializes every iteration on stderr and costs about two orders of
	// magnitude of throughput.
	logrus.SetOutput(io.Discard)

	dir := f.TempDir()
	rootfs := filepath.Join(dir, "rootfs")
	if err := os.MkdirAll(rootfs, 0o750); err != nil {
		f.Fatal(err)
	}

	return dir, filepath.Join(rootfs, uruncJSONFilename), &specs.Spec{
		Root:        &specs.Root{Path: rootfs},
		Annotations: map[string]string{},
	}
}

// FuzzConfigMandatoryFields asserts the contract that validate() exists to
// enforce: any config GetUnikernelConfig hands back must carry non-empty
// unikernelType, hypervisor and binary. Sixteen references in
// unikontainers.go read those three straight out of the state annotations
// without rechecking them, and Map() silently drops empty values, so an
// accepted config with an empty mandatory field corrupts container state
// rather than failing loudly.
//
// Input is the four field values, so the fuzzer mutates the base64 payloads
// directly instead of having to synthesize the surrounding JSON. That makes
// it far more effective than FuzzGetUnikernelConfigJSON at reaching the
// decoder, at the cost of not exercising JSON parsing itself.
func FuzzConfigMandatoryFields(f *testing.F) {
	f.Add(b64("unikraft"), b64("qemu"), b64("/unikernel/app"), b64("nginx -c /nginx.conf"))
	f.Add(b64("rumprun"), b64("hvt"), b64("/unikernel/app.hvt"), b64(""))
	f.Add("", "", "", "")
	f.Add("not-base64!", "qemu", "bin", "")

	dir, jsonPath, spec := fuzzBundle(f)

	f.Fuzz(func(t *testing.T, unikernelType, hypervisor, binary, cmdline string) {
		data, err := json.Marshal(map[string]string{
			annotType:       unikernelType,
			annotHypervisor: hypervisor,
			annotBinary:     binary,
			annotCmdLine:    cmdline,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(jsonPath, data, 0o600); err != nil {
			t.Fatal(err)
		}

		conf, err := GetUnikernelConfig(dir, spec)
		if err != nil {
			return // rejected, a valid outcome for arbitrary input
		}
		if conf == nil {
			t.Fatal("GetUnikernelConfig returned a nil config and a nil error")
		}

		if conf.UnikernelType == "" || conf.Hypervisor == "" || conf.UnikernelBinary == "" {
			t.Fatalf("accepted config is missing a mandatory field\n"+
				"encoded: type=%q hypervisor=%q binary=%q\n"+
				"decoded: type=%q hypervisor=%q binary=%q",
				unikernelType, hypervisor, binary,
				conf.UnikernelType, conf.Hypervisor, conf.UnikernelBinary)
		}
	})
}

// FuzzGetUnikernelConfigJSON drives arbitrary urunc.json bytes through the
// public entry point. It asserts that a config urunc accepts always carries
// the mandatory fields that downstream code reads out of the state
// annotations, and that no input panics the parser.
func FuzzGetUnikernelConfigJSON(f *testing.F) {
	seed, err := json.Marshal(map[string]string{
		annotType:       b64("unikraft"),
		annotHypervisor: b64("qemu"),
		annotBinary:     b64("/unikernel/app"),
		annotCmdLine:    b64("nginx"),
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(`{}`))
	f.Add([]byte(`not json`))

	dir, jsonPath, spec := fuzzBundle(f)

	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(jsonPath, data, 0o600); err != nil {
			t.Fatal(err)
		}

		conf, err := GetUnikernelConfig(dir, spec)
		if err != nil {
			return // rejected, which is a valid outcome for arbitrary input
		}
		if conf == nil {
			t.Fatal("GetUnikernelConfig returned a nil config and a nil error")
		}

		if conf.UnikernelType == "" || conf.Hypervisor == "" || conf.UnikernelBinary == "" {
			t.Fatalf("accepted config is missing a mandatory field: "+
				"type=%q hypervisor=%q binary=%q\ninput: %q",
				conf.UnikernelType, conf.Hypervisor, conf.UnikernelBinary, data)
		}
	})
}
