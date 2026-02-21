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
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const UruncConfigPath = "/etc/urunc/config.toml"

type UruncLog struct {
	Level  string `toml:"level"`
	Syslog bool   `toml:"syslog"`
}

type UruncTimestamps struct {
	Enabled     bool   `toml:"enabled"`
	Destination string `toml:"destination"` // Used to specify a file for timestamps
}

type UruncConfig struct {
	Log        UruncLog                        `toml:"log"`
	Timestamps UruncTimestamps                 `toml:"timestamps"`
	Monitors   map[string]types.MonitorConfig  `toml:"monitors"`
	ExtraBins  map[string]types.ExtraBinConfig `toml:"extra_binaries"`
}

// this struct is used to parse only the log and timestamp section of the urunc config file
type LogMetricsUruncConfig struct {
	Log        UruncLog        `toml:"log"`
	Timestamps UruncTimestamps `toml:"timestamps"`
}

func ParseLogMetricsConfig(path string) (LogMetricsUruncConfig, error) {
	var initialConf LogMetricsUruncConfig
	_, err := toml.DecodeFile(path, &initialConf)
	if err == nil {
		return initialConf, nil
	}
	uniklog.Warnf("Failed to load urunc log/metrics config from %s: %v. Using default configuration.", path, err)
	return defaultLogMetricsConfig(), err
}

func defaultLogMetricsConfig() LogMetricsUruncConfig {
	return LogMetricsUruncConfig{
		Log:        defaultLogConfig(),
		Timestamps: defaultTimestampsConfig(),
	}
}

func defaultLogConfig() UruncLog {
	return UruncLog{
		Level:  "info",
		Syslog: false,
	}
}

func defaultTimestampsConfig() UruncTimestamps {
	return UruncTimestamps{
		Enabled:     false,
		Destination: "/var/log/urunc/timestamps.log",
	}
}

func defaultMonitorsConfig() map[string]types.MonitorConfig {
	return map[string]types.MonitorConfig{
		"qemu":             {DefaultMemoryMB: 256, DefaultVCPUs: 1},
		"hvt":              {DefaultMemoryMB: 256, DefaultVCPUs: 1},
		"spt":              {DefaultMemoryMB: 256, DefaultVCPUs: 1},
		"firecracker":      {DefaultMemoryMB: 256, DefaultVCPUs: 1},
		"cloud-hypervisor": {DefaultMemoryMB: 256, DefaultVCPUs: 1},
	}
}

func defaultExtraBinConfig() map[string]types.ExtraBinConfig {
	return map[string]types.ExtraBinConfig{
		"virtiofsd": {Path: "/usr/libexec/virtiofsd", Options: "--cache always --sandbox none"},
	}
}

func defaultUruncConfig() *UruncConfig {
	return &UruncConfig{
		Log:        defaultLogConfig(),
		Timestamps: defaultTimestampsConfig(),
		Monitors:   defaultMonitorsConfig(),
		ExtraBins:  defaultExtraBinConfig(),
	}
}

// LoadUruncConfig loads the urunc configuration from the specified path.
// If the file does not exist or is malformed, it returns the default configuration.
func LoadUruncConfig(path string) (*UruncConfig, error) {
	cfg := &UruncConfig{}
	_, err := toml.DecodeFile(path, cfg)
	if err == nil {
		return cfg, nil
	}
	uniklog.Warnf("Failed to load urunc config from %s: %v. Using default configuration.", path, err)
	return defaultUruncConfig(), err
}

func (p *UruncConfig) Map() map[string]string {
	// since log and timestamps are loaded at the start of urunc, we will not be adding
	// them to this map. this map will be used to save the rest of the urunc config to state.json
	cfgMap := make(map[string]string)

	for hv, hvCfg := range p.Monitors {
		prefix := "urunc_config.monitors." + hv + "."
		cfgMap[prefix+"default_memory_mb"] = strconv.FormatUint(uint64(hvCfg.DefaultMemoryMB), 10)
		cfgMap[prefix+"default_vcpus"] = strconv.FormatUint(uint64(hvCfg.DefaultVCPUs), 10)
		cfgMap[prefix+"binary_path"] = hvCfg.BinaryPath
		cfgMap[prefix+"data_path"] = hvCfg.DataPath
		cfgMap[prefix+"vhost"] = strconv.FormatBool(hvCfg.Vhost)
	}
	for eb, ebCfg := range p.ExtraBins {
		prefix := "urunc_config.extra_binaries." + eb + "."
		cfgMap[prefix+"path"] = ebCfg.Path
		cfgMap[prefix+"options"] = ebCfg.Options
	}
	return cfgMap
}

func UruncConfigFromMap(cfgMap map[string]string) *UruncConfig {
	// since log and timestamps are loaded at the start of urunc, we will not be reading
	// them from this map. this map will be used to parse the rest of the urunc config from state.json
	cfg := &UruncConfig{
		Monitors:  defaultMonitorsConfig(),
		ExtraBins: defaultExtraBinConfig(),
	}

	for key, val := range cfgMap {
		if !strings.HasPrefix(key, "urunc_config.monitors.") {
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) != 4 {
			continue
		}
		hv := parts[2]
		if cfg.Monitors == nil {
			cfg.Monitors = make(map[string]types.MonitorConfig)
		}
		hvCfg, exists := cfg.Monitors[hv]
		if !exists {
			hvCfg = types.MonitorConfig{}
		}
		switch parts[3] {
		case "default_memory_mb":
			if intVal, err := strconv.Atoi(val); err == nil && intVal > 0 {
				hvCfg.DefaultMemoryMB = uint(intVal)
			}
		case "default_vcpus":
			if intVal, err := strconv.Atoi(val); err == nil && intVal > 0 {
				hvCfg.DefaultVCPUs = uint(intVal)
			}
		case "binary_path":
			hvCfg.BinaryPath = val
		case "data_path":
			hvCfg.DataPath = val
		case "vhost":
			boolVal, err := strconv.ParseBool(val)
			if err != nil {
				uniklog.Warnf("Invalid vhost value '%s' for monitor '%s': %v. Using default (false).", val, hv, err)
			} else {
				hvCfg.Vhost = boolVal
			}
		}
		cfg.Monitors[hv] = hvCfg
	}
	for key, val := range cfgMap {
		if !strings.HasPrefix(key, "urunc_config.extra_binaries.") {
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) != 4 {
			continue
		}
		eb := parts[2]
		if cfg.ExtraBins == nil {
			cfg.ExtraBins = make(map[string]types.ExtraBinConfig)
		}
		ebCfg, exists := cfg.ExtraBins[eb]
		if !exists {
			ebCfg = types.ExtraBinConfig{}
		}
		switch parts[3] {
		case "path":
			ebCfg.Path = val
		case "options":
			ebCfg.Options = val
		}
		cfg.ExtraBins[eb] = ebCfg
	}
	return cfg
}
