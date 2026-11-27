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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const UruncConfigPath = "/etc/urunc/config.toml"

// UruncConfigFileEnv is the environment variable that overrides the default
// location of the urunc configuration file.
const UruncConfigFileEnv = "URUNC_CONFIG_FILE"

// ResolveUruncConfigPath returns the path to the urunc configuration file. It
// uses the URUNC_CONFIG_FILE environment variable when it is set and falls back
// to UruncConfigPath (/etc/urunc/config.toml) otherwise.
func ResolveUruncConfigPath() string {
	if path := os.Getenv(UruncConfigFileEnv); path != "" {
		return path
	}
	return UruncConfigPath
}

type UruncLog struct {
	Level  string `toml:"level"`
	Syslog bool   `toml:"syslog"`
}

type UruncTimestamps struct {
	Enabled     bool   `toml:"enabled"`
	Destination string `toml:"destination"` // Used to specify a file for timestamps
}

// UruncRuntime holds runtime-wide behavior options.
type UruncRuntime struct {
	// Libcontainer selects whether the monitor's execution environment
	// is set up through runc's libcontainer instead of urunc's own implementation.
	Libcontainer bool `toml:"libcontainer"`
	// VAccel is required to accept vAccel related annotations.
	VAccel bool `toml:"vAccel"`
}

// state.json annotation keys with the runtime settings.
const (
	libcontainerKey = "urunc_config.runtime.libcontainer"
	vaccelKey       = "urunc_config.runtime.vAccel"
)

type UruncConfig struct {
	Log        UruncLog                        `toml:"log"`
	Timestamps UruncTimestamps                 `toml:"timestamps"`
	Runtime    UruncRuntime                    `toml:"runtime"`
	Monitors   map[string]types.MonitorConfig  `toml:"monitors"`
	ExtraBins  map[string]types.ExtraBinConfig `toml:"extra_binaries"`
}

// runtimeFromMap rebuilds the runtime options from the state.json annotations.
// A missing or malformed value falls back to the default (off).
func runtimeFromMap(cfgMap map[string]string) UruncRuntime {
	rt := defaultRuntimeConfig()
	if val, ok := cfgMap[libcontainerKey]; ok {
		choice, err := strconv.ParseBool(val)
		if err != nil {
			uniklog.Warnf("Invalid libcontainer value %q. Using default (false).", val)
		} else {
			rt.Libcontainer = choice
		}
	}
	if val, ok := cfgMap[vaccelKey]; ok {
		choice, err := strconv.ParseBool(val)
		if err != nil {
			uniklog.Warnf("Invalid vAccel value %q. Using default (false).", val)
		} else {
			rt.VAccel = choice
		}
	}
	return rt
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
	// Only the logs and the timestamps use the defaults. urunc create still
	// fails on a file that can not be read or decoded.
	uniklog.Warnf("Failed to load urunc config from %s: %v. Using the default log and timestamp configuration.", path, err)
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

func defaultRuntimeConfig() UruncRuntime {
	return UruncRuntime{
		Libcontainer: false,
		VAccel:       false,
	}
}

const (
	defaultMonitorMemoryMB uint = 256
	defaultMonitorVCPUs    uint = 1
)

func defaultMonitorsConfig() map[string]types.MonitorConfig {
	return map[string]types.MonitorConfig{
		"qemu":                {DefaultMemoryMB: defaultMonitorMemoryMB, DefaultVCPUs: defaultMonitorVCPUs},
		"hvt":                 {DefaultMemoryMB: defaultMonitorMemoryMB, DefaultVCPUs: defaultMonitorVCPUs},
		"spt":                 {DefaultMemoryMB: defaultMonitorMemoryMB, DefaultVCPUs: defaultMonitorVCPUs},
		"firecracker":         {DefaultMemoryMB: defaultMonitorMemoryMB, DefaultVCPUs: defaultMonitorVCPUs},
		"cloud-hypervisor":    {DefaultMemoryMB: defaultMonitorMemoryMB, DefaultVCPUs: defaultMonitorVCPUs},
		"hyperlight-unikraft": {DefaultMemoryMB: defaultMonitorMemoryMB, DefaultVCPUs: defaultMonitorVCPUs},
		"hvi":                 {DefaultMemoryMB: defaultMonitorMemoryMB, DefaultVCPUs: defaultMonitorVCPUs},
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
		Runtime:    defaultRuntimeConfig(),
		Monitors:   defaultMonitorsConfig(),
		ExtraBins:  defaultExtraBinConfig(),
	}
}

// LoadUruncConfig loads the urunc configuration from the specified path.
// If the file does not exist, it returns the default configuration. If the
// file can not be read or decoded, or holds an invalid path, it returns an
// error.
func LoadUruncConfig(path string) (*UruncConfig, error) {
	cfg := defaultUruncConfig()
	_, err := toml.DecodeFile(path, cfg)
	if errors.Is(err, fs.ErrNotExist) {
		uniklog.Warnf("urunc config %s does not exist. Using default configuration.", path)
		return cfg, nil
	}
	if err != nil {
		// The defaults would silently replace every field of the file.
		return nil, fmt.Errorf("failed to load urunc config from %s: %w", path, err)
	}
	// Decoding a partially-specified [monitors.<name>] section (e.g. one that
	// only sets path) zeroes the fields absent from the file, dropping
	// the seeded defaults. Re-apply them for any zero value.
	// The errors of every monitor and extra binary are collected, so that a
	// single error reports all of them.
	var errs []error
	for name, mon := range cfg.Monitors {
		if mon.DefaultMemoryMB == 0 {
			mon.DefaultMemoryMB = defaultMonitorMemoryMB
		}
		if mon.DefaultVCPUs == 0 {
			mon.DefaultVCPUs = defaultMonitorVCPUs
		}
		// Every monitor is checked, even one that no container uses.
		err = checkMonitorPaths(mon)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid monitor %s: %w", name, err))
		}
		cfg.Monitors[name] = mon
	}
	// Same issue for [extra_binaries.virtiofsd], the only supported extra binary today.
	if bin, ok := cfg.ExtraBins["virtiofsd"]; ok {
		def := defaultExtraBinConfig()["virtiofsd"]
		if bin.Path == "" {
			bin.Path = def.Path
		}
		err = checkConfigPath("path", bin.Path)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid extra binary virtiofsd: %w", err))
		}
		err = checkHostLibs(bin.HostLibs)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid extra binary virtiofsd: %w", err))
		}
		if bin.Options == "" {
			bin.Options = def.Options
		}
		cfg.ExtraBins["virtiofsd"] = bin
	}
	err = errors.Join(errs...)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// checkMonitorPaths returns an error if a path field of a monitor is set but
// is not absolute and clean.
func checkMonitorPaths(mon types.MonitorConfig) error {
	if mon.BinaryPath != "" {
		err := checkConfigPath("path", mon.BinaryPath)
		if err != nil {
			return err
		}
	}
	if mon.SocketPath != "" {
		err := checkConfigPath("socket_path", mon.SocketPath)
		if err != nil {
			return err
		}
	}
	return checkHostLibs(mon.HostLibs)
}

// checkHostLibs checks the host_libs field of a monitor or an extra binary.
//
// Arguments:
//   - libs: the host paths that urunc mounts in the monitor rootfs.
//
// Returns:
//   - an error for the first path that is not absolute and clean.
func checkHostLibs(libs []string) error {
	for _, lib := range libs {
		err := checkConfigPath("host_libs", lib)
		if err != nil {
			return err
		}
	}
	return nil
}

// checkConfigPath returns an error if path, the value of a path field of the
// configuration, is not absolute and clean. urunc uses these paths at the same
// place in the monitor rootfs, where a relative path has no meaning. Clean
// paths can also be compared as strings.
func checkConfigPath(field, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%s %q is not an absolute and clean path", field, path)
	}
	return nil
}

func (p *UruncConfig) Map() map[string]string {
	// since log and timestamps are loaded at the start of urunc, we will not be adding
	// them to this map. this map will be used to save the rest of the urunc config to state.json
	// The host libraries are not added either, since only create uses them,
	// with the configuration from the file.
	cfgMap := make(map[string]string)

	cfgMap[libcontainerKey] = strconv.FormatBool(p.Runtime.Libcontainer)
	cfgMap[vaccelKey] = strconv.FormatBool(p.Runtime.VAccel)
	for hv, hvCfg := range p.Monitors {
		prefix := "urunc_config.monitors." + hv + "."
		cfgMap[prefix+"default_memory_mb"] = strconv.FormatUint(uint64(hvCfg.DefaultMemoryMB), 10)
		cfgMap[prefix+"default_vcpus"] = strconv.FormatUint(uint64(hvCfg.DefaultVCPUs), 10)
		cfgMap[prefix+"binary_path"] = hvCfg.BinaryPath
		cfgMap[prefix+"data_path"] = hvCfg.DataPath
		cfgMap[prefix+"vhost"] = strconv.FormatBool(hvCfg.Vhost)
		cfgMap[prefix+"socket_path"] = hvCfg.SocketPath
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
		Runtime:   runtimeFromMap(cfgMap),
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
		case "socket_path":
			hvCfg.SocketPath = val
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
