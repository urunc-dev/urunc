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
	"encoding/json"
	"os"
	"testing"
)

func FuzzUruncConfigFromMap(f *testing.F) {
	// Seed corpus with a valid configuration snippet
	f.Add("urunc_config.monitors.qemu.default_memory_mb", "512")
	f.Add("urunc_config.monitors.hvt.binary_path", "/usr/bin/hvt")
	f.Add("urunc_config.extra_binaries.virtiofsd.options", "--cache none")

	f.Fuzz(func(t *testing.T, key string, val string) {
		m := map[string]string{
			key: val,
		}
		// The primary goal is that parsing does not panic or hang
		_ = UruncConfigFromMap(m)
	})
}

func FuzzUnikernelConfigDecode(f *testing.F) {
	// Add some seeds including the ones that Cypher-CP0 mentioned cause silent corruption
	f.Add("qemu")
	f.Add("unikraft")
	f.Add("mewz")
	f.Add("cWVtdQ==") // "qemu" base64 encoded

	f.Fuzz(func(t *testing.T, input string) {
		cfg := UnikernelConfig{
			UnikernelType:    input,
			UnikernelVersion: input,
			UnikernelBinary:  input,
			Hypervisor:       input,
		}
		
		// UnikernelConfig.decode() shouldn't panic
		_ = cfg.decode()
	})
}

func FuzzGetUnikernelConfig(f *testing.F) {
	// Fuzz the getConfigFromJSON which takes a file path
	f.Add([]byte(`{"com.urunc.unikernel.unikernelType":"cWVtdQ=="}`))
	
	f.Fuzz(func(t *testing.T, data []byte) {
		// Verify JSON is somewhat structurally valid to avoid disk I/O on complete garbage
		var dummy map[string]interface{}
		if err := json.Unmarshal(data, &dummy); err != nil {
			return
		}

		tmpFile, err := os.CreateTemp(t.TempDir(), "urunc-fuzz-*.json")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.Write(data); err != nil {
			t.Fatalf("failed to write to temp file: %v", err)
		}
		if err := tmpFile.Close(); err != nil {
			t.Fatalf("failed to close temp file: %v", err)
		}

		// Run the config unmarshaller
		_, _ = getConfigFromJSON(tmpFile.Name())
	})
}
