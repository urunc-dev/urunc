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
	"io"
	"maps"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/urunc-dev/urunc/internal/fuzzseed"
)

// FuzzUruncConfigFromMap builds the urunc config from arbitrary state.json annotations.
func FuzzUruncConfigFromMap(f *testing.F) {
	// Every field but vAccel, each with its own value.
	fuzzseed.Add(f, []byte(`{"`+libcontainerKey+`":"1",`+
		`"urunc_config.monitors.qemu.default_memory_mb":"512","urunc_config.monitors.qemu.default_vcpus":"2",`+
		`"urunc_config.monitors.qemu.binary_path":"/usr/bin/qemu","urunc_config.monitors.qemu.data_path":"/usr/share/qemu",`+
		`"urunc_config.monitors.qemu.socket_path":"/run/qemu.sock","urunc_config.monitors.qemu.vhost":"true",`+
		`"urunc_config.extra_binaries.virtiofsd.path":"/bin/virtiofsd","urunc_config.extra_binaries.virtiofsd.options":"--cache never"}`))
	// vAccel, values that keep the default (numbers below 1, bad bools), a number over 32 bits,
	// and keys that only look like urunc config keys.
	fuzzseed.Add(f, []byte(`{"`+vaccelKey+`":"true","`+libcontainerKey+`":"yes","urunc_config.monitors.spt.vhost":"yes",`+
		`"urunc_config.monitors.hvt.default_memory_mb":"0","urunc_config.monitors.hvt.default_vcpus":"0",`+
		`"urunc_config.monitors.spt.default_memory_mb":"-4","urunc_config.monitors.spt.default_vcpus":"-4",`+
		`"urunc_config.monitors.firecracker.default_memory_mb":"8589934592",`+
		`"urunc_config.monitors.qemu.default_vcpus.extra":"8","x.monitors.qemu.default_vcpus":"8",`+
		`"urunc_config.extra_binaries.virtiofsd.path.extra":"/bin/other","x.extra_binaries.virtiofsd.path":"/bin/other"}`))
	// urunc warns on every bad bool, which would flood the fuzzer's log.
	logrus.SetOutput(io.Discard)
	f.Cleanup(func() { logrus.SetOutput(os.Stderr) })

	f.Fuzz(func(t *testing.T, data []byte) {
		var annotations map[string]string
		if json.Unmarshal(data, &annotations) != nil {
			return
		}
		cfg := UruncConfigFromMap(annotations)

		// The runtime options are on only for a value strconv accepts as true. An invalid
		// value keeps the default, and every bool defaults to false, so the parse error can be dropped.
		for key, got := range map[string]bool{libcontainerKey: cfg.Runtime.Libcontainer, vaccelKey: cfg.Runtime.VAccel} {
			if want, _ := strconv.ParseBool(annotations[key]); got != want {
				t.Fatalf("%s is %v for %q", key, got, annotations[key])
			}
		}

		// A field an entry names holds its value, a number only if it is positive.
		named := map[string]bool{}
		for key, val := range annotations {
			parts := strings.Split(key, ".")
			if len(parts) != 4 || parts[0] != "urunc_config" {
				continue
			}
			named[parts[1]+"."+parts[2]] = true
			mon, bin := cfg.Monitors[parts[2]], cfg.ExtraBins[parts[2]]
			n, err := strconv.Atoi(val)
			positive := err == nil && n > 0
			var wrong bool
			switch parts[1] + "." + parts[3] {
			case "monitors.default_memory_mb":
				wrong = positive && mon.DefaultMemoryMB != uint(n)
			case "monitors.default_vcpus":
				wrong = positive && mon.DefaultVCPUs != uint(n)
			case "monitors.binary_path":
				wrong = mon.BinaryPath != val
			case "monitors.data_path":
				wrong = mon.DataPath != val
			case "monitors.socket_path":
				wrong = mon.SocketPath != val
			case "monitors.vhost":
				want, _ := strconv.ParseBool(val)
				wrong = mon.Vhost != want
			case "extra_binaries.path":
				wrong = bin.Path != val
			case "extra_binaries.options":
				wrong = bin.Options != val
			}
			if wrong {
				t.Fatalf("%q=%q not applied: %+v %+v", key, val, mon, bin)
			}
		}

		// Defaults no entry names are kept, and no default monitor is left without memory or vCPUs.
		for name, def := range defaultMonitorsConfig() {
			mon := cfg.Monitors[name]
			if mon.DefaultMemoryMB == 0 || mon.DefaultVCPUs == 0 {
				t.Fatalf("monitor %s has no memory or vCPUs: %+v", name, mon)
			}
			if !named["monitors."+name] && mon != def {
				t.Fatalf("monitor %s is %+v, want its default %+v", name, mon, def)
			}
		}
		for name, def := range defaultExtraBinConfig() {
			if !named["extra_binaries."+name] && cfg.ExtraBins[name] != def {
				t.Fatalf("extra binary %s is %+v, want its default %+v", name, cfg.ExtraBins[name], def)
			}
		}

		// Writing the config out and reading it again changes nothing.
		again := UruncConfigFromMap(cfg.Map())
		if again.Runtime != cfg.Runtime || !maps.Equal(again.Monitors, cfg.Monitors) || !maps.Equal(again.ExtraBins, cfg.ExtraBins) {
			t.Fatalf("round trip changed the config:\n%+v\n%+v", cfg, again)
		}
	})
}
