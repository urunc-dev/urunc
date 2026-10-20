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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzUruncConfigTOML (decode-only, crash check) was retired: found nothing,
// and TOML parsing is BurntSushi/toml's problem, not urunc's.

// FuzzUruncConfigFromMap feeds arbitrary "key=value" lines through
// UruncConfigFromMap, the state-restore counterpart to LoadUruncConfig. The
// map is round-tripped through a blob since f.Add can't take map[string]string.
func FuzzUruncConfigFromMap(f *testing.F) {
	seeds := []string{
		"",
		"urunc_config.monitors.qemu.default_memory_mb=512",
		"urunc_config.monitors.qemu.default_vcpus=2\nurunc_config.monitors.qemu.binary_path=/usr/bin/qemu",
		"urunc_config.monitors.qemu.vhost=true",
		"urunc_config.monitors.qemu.vhost=not-a-bool",
		"urunc_config.monitors.qemu.default_memory_mb=-1",
		"urunc_config.monitors.qemu.default_memory_mb=99999999999999999999",
		"urunc_config.extra_binaries.virtiofsd.path=/a\nurunc_config.extra_binaries.virtiofsd.options=--cache always",
		"not_urunc_config_prefixed=value",
		"urunc_config.monitors.a.b.c.d=x",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, blob string) {
		if !strictOn("config-frommap") {
			return
		}
		cfgMap := make(map[string]string)
		for _, line := range strings.Split(blob, "\n") {
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			cfgMap[key] = val
		}

		cfg := UruncConfigFromMap(cfgMap)
		if cfg == nil {
			t.Fatalf("UruncConfigFromMap(%v) returned nil", cfgMap)
		}
		// Map() must not panic on whatever UruncConfigFromMap built from
		// arbitrary keys/values.
		_ = cfg.Map()

		// LoadUruncConfig re-applies defaultMonitorMemoryMB/defaultMonitorVCPUs
		// to any monitor left at zero; UruncConfigFromMap does not.
		for hv, mon := range cfg.Monitors {
			if mon.DefaultMemoryMB == 0 || mon.DefaultVCPUs == 0 {
				t.Fatalf("UruncConfigFromMap(%v) left monitor %q with a zero default (mem=%d vcpus=%d)",
					cfgMap, hv, mon.DefaultMemoryMB, mon.DefaultVCPUs)
			}
		}
	})
}

// FuzzUruncConfigRoundTrip exercises the real create->start config handoff for
// the host config (/etc/urunc/config.toml): LoadUruncConfig decodes the TOML at
// create; Map() writes the result into state.json; UruncConfigFromMap() rebuilds
// it at start. The container is configured by the START-side reconstruction, so
// if it diverges from what create loaded, the running VM is configured
// differently than the operator's file specified. Inputs are typed and rendered
// into a valid TOML document (realism filter below) so the target exercises
// urunc's own load/serialise/restore logic rather than BurntSushi/toml internals.
func FuzzUruncConfigRoundTrip(f *testing.F) {
	// mon, binPath, mem, vcpu, vhost, libcontainer, vaccel, extraBin, extraBinPath
	f.Add("qemu", "/opt/urunc/bin/qemu-system-x86_64", uint32(256), uint32(2), false, true, false, "virtiofsd", "/opt/urunc/bin/virtiofsd")
	f.Add("my.mon", "/x", uint32(512), uint32(4), true, false, true, "virtiofsd", "/y")  // dotted monitor name -> known divergence
	f.Add("customhv", "/c", uint32(128), uint32(1), false, false, false, "eb.dot", "/z") // dotted extra-binary name
	f.Add("spt", "", uint32(1), uint32(1), false, true, true, "vfsd", "")

	okName := func(s string) bool {
		if s == "" || len(s) > 64 {
			return false
		}
		for _, r := range s { // operator-authored TOML section names: keep it to realistic chars
			switch {
			case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-', r == ' ':
			default:
				return false
			}
		}
		return true
	}
	okVal := func(s string) bool { return len(s) <= 256 && !strings.ContainsAny(s, "\"\\\n\r\t") }

	f.Fuzz(func(t *testing.T, mon, binPath string, mem, vcpu uint32, vhost, libc, vaccel bool, eb, ebPath string) {
		if !strictOn("config-roundtrip") {
			return
		}
		if !okName(mon) || !okName(eb) || !okVal(binPath) || !okVal(ebPath) {
			return
		}
		if mem == 0 || vcpu == 0 {
			return // 0 encodes "unset" in the state map (Atoi>0 guard); not a round-trip question
		}
		// Render a valid TOML config. MonitorConfig.BinaryPath has toml tag "path".
		doc := fmt.Sprintf("[runtime]\nlibcontainer = %t\nvAccel = %t\n"+
			"[monitors.%q]\npath = %q\ndefault_memory_mb = %d\ndefault_vcpus = %d\nvhost = %t\n"+
			"[extra_binaries.%q]\npath = %q\noptions = \"\"\n",
			libc, vaccel, mon, binPath, mem, vcpu, vhost, eb, ebPath)

		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil { //nolint:gosec // a scratch file
			t.Skip(err)
		}
		loaded, err := LoadUruncConfig(path)
		if err != nil {
			return // a TOML decode error is the library's domain, not urunc's
		}
		if _, ok := loaded.Monitors[mon]; !ok {
			return // this name isn't carried as a TOML key here; nothing urunc-specific to assert
		}

		// Oracle 1: LoadUruncConfig re-applies mem/vcpu defaults to every monitor.
		for name, m := range loaded.Monitors {
			if m.DefaultMemoryMB == 0 || m.DefaultVCPUs == 0 {
				t.Fatalf("LoadUruncConfig left monitor %q at a zero default (mem=%d vcpu=%d)", name, m.DefaultMemoryMB, m.DefaultVCPUs)
			}
		}

		// Oracle 2 (the real one): the create->start state round-trip must preserve
		// every monitor, extra binary, and runtime flag urunc loaded at create.
		start := UruncConfigFromMap(loaded.Map())
		if start.Runtime != loaded.Runtime {
			t.Fatalf("runtime flags diverge create->start:\n  create=%+v\n  start =%+v", loaded.Runtime, start.Runtime)
		}
		for name, want := range loaded.Monitors {
			got, ok := start.Monitors[name]
			if !ok || got != want {
				t.Fatalf("monitor %q diverges across the create->start state round-trip:\n  create=%+v\n  start =%+v (present=%v)", name, want, got, ok)
			}
		}
		for name, want := range loaded.ExtraBins {
			got, ok := start.ExtraBins[name]
			if !ok || got != want {
				t.Fatalf("extra binary %q diverges across the create->start state round-trip:\n  create=%+v\n  start =%+v (present=%v)", name, want, got, ok)
			}
		}
	})
}
