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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// Constants for test configuration keys and values
const (
	testQemuMemoryKey    = "urunc_config.monitors.qemu.default_memory_mb"
	testQemuVCPUsKey     = "urunc_config.monitors.qemu.default_vcpus"
	testQemuBinaryKey    = "urunc_config.monitors.qemu.binary_path"
	testQemuDataKey      = "urunc_config.monitors.qemu.data_path"
	testQemuVhostKey     = "urunc_config.monitors.qemu.vhost"
	testHvtMemoryKey     = "urunc_config.monitors.hvt.default_memory_mb"
	testVirtiofsdPathKey = "urunc_config.extra_binaries.virtiofsd.path"
	testVirtiofsdOptsKey = "urunc_config.extra_binaries.virtiofsd.options"
	testVirtiofsdDefOpts = "--cache always --sandbox none"
	testBinOpts          = "opt1 opt2"
	testQemuBinaryPath   = "/usr/bin/qemu"
	testQemuDataPath     = "/usr/local/share/qemu"
	testTimestampsPath   = "/var/log/urunc/timestamps.log"
)

func TestUruncConfigFromMap(t *testing.T) {
	t.Run("empty map returns default config", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Equal(t, defaultMonitorsConfig(), config.Monitors)
		assert.Equal(t, defaultExtraBinConfig(), config.ExtraBins)
	})

	t.Run("single monitor with all fields", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey: "512",
			testQemuVCPUsKey:  "2",
			testQemuBinaryKey: testQemuBinaryPath,
			testQemuDataKey:   testQemuDataPath,
			testQemuVhostKey:  "true",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.Monitors, "qemu")
		qemuConfig := config.Monitors["qemu"]
		assert.Equal(t, uint(512), qemuConfig.DefaultMemoryMB)
		assert.Equal(t, uint(2), qemuConfig.DefaultVCPUs)
		assert.Equal(t, testQemuBinaryPath, qemuConfig.BinaryPath)
		assert.Equal(t, testQemuDataPath, qemuConfig.DataPath)
		assert.True(t, qemuConfig.Vhost)
	})

	t.Run("multiple monitors", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey: "512",
			testQemuVCPUsKey:  "2",
			"urunc_config.monitors.firecracker.default_memory_mb": "128",
			"urunc_config.monitors.firecracker.binary_path":       "/usr/bin/firecracker",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.Monitors, "qemu")
		assert.Contains(t, config.Monitors, "firecracker")

		qemuConfig := config.Monitors["qemu"]
		assert.Equal(t, uint(512), qemuConfig.DefaultMemoryMB)
		assert.Equal(t, uint(2), qemuConfig.DefaultVCPUs)

		firecrackerConfig := config.Monitors["firecracker"]
		assert.Equal(t, uint(128), firecrackerConfig.DefaultMemoryMB)
		assert.Equal(t, "/usr/bin/firecracker", firecrackerConfig.BinaryPath)
	})

	t.Run("partial monitor config", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testHvtMemoryKey: "1024",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.Monitors, "hvt")
		hvtConfig := config.Monitors["hvt"]
		assert.Equal(t, uint(1024), hvtConfig.DefaultMemoryMB)
		assert.Equal(t, uint(1), hvtConfig.DefaultVCPUs) // Default value for unset field
		assert.Equal(t, "", hvtConfig.BinaryPath)
	})

	t.Run("invalid or negative numeric values are ignored", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey: "invalid",
			testQemuVCPUsKey:  "-5",
			testQemuBinaryKey: testQemuBinaryPath,
			testQemuDataKey:   testQemuDataPath,
			"urunc_config.monitors.qemu.field.extra.parts": "invalid",
			testHvtMemoryKey: "512",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.Monitors, "qemu")
		qemuConfig := config.Monitors["qemu"]
		assert.Equal(t, uint(256), qemuConfig.DefaultMemoryMB) // Default value for invalid input
		assert.Equal(t, uint(1), qemuConfig.DefaultVCPUs)      // Default value for negative input
		assert.Equal(t, testQemuBinaryPath, qemuConfig.BinaryPath)
		assert.Equal(t, testQemuDataPath, qemuConfig.DataPath)
		assert.Contains(t, config.Monitors, "hvt")
		hvtConfig := config.Monitors["hvt"]
		assert.Equal(t, uint(512), hvtConfig.DefaultMemoryMB)
	})

	t.Run("unknown monitor field is ignored", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			"urunc_config.monitors.qemu.unknown_field": "value",
			testQemuMemoryKey:                          "512",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		qemuConfig := config.Monitors["qemu"]
		assert.Equal(t, uint(512), qemuConfig.DefaultMemoryMB)
	})

	t.Run("new monitor not in default config", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			"urunc_config.monitors.custom.default_memory_mb": "2048",
			"urunc_config.monitors.custom.default_vcpus":     "4",
			"urunc_config.monitors.custom.binary_path":       "/custom/hypervisor",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.Monitors, "custom")
		customConfig := config.Monitors["custom"]
		assert.Equal(t, uint(2048), customConfig.DefaultMemoryMB)
		assert.Equal(t, uint(4), customConfig.DefaultVCPUs)
		assert.Equal(t, "/custom/hypervisor", customConfig.BinaryPath)
	})

	t.Run("mixed valid and invalid entries", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey:                         "512",
			"invalid.key.format":                      "ignored",
			"urunc_config.monitors.hvt.default_vcpus": "invalid_number",
			"urunc_config.monitors.spt.binary_path":   "/usr/bin/spt",
			"urunc_config.monitors":                   "malformed",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)

		// qemu should have memory set
		qemuConfig := config.Monitors["qemu"]
		assert.Equal(t, uint(512), qemuConfig.DefaultMemoryMB)

		// hvt should preserve default vcpus value due to invalid input
		hvtConfig := config.Monitors["hvt"]
		assert.Equal(t, uint(1), hvtConfig.DefaultVCPUs)

		// spt should have binary path set
		sptConfig := config.Monitors["spt"]
		assert.Equal(t, "/usr/bin/spt", sptConfig.BinaryPath)
	})

	t.Run("preserves default monitors not in map", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey: "512",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		// Should still contain all default monitors
		assert.Contains(t, config.Monitors, "qemu")
		assert.Contains(t, config.Monitors, "hvt")
		assert.Contains(t, config.Monitors, "spt")
		assert.Contains(t, config.Monitors, "firecracker")

		// qemu should be modified
		qemuConfig := config.Monitors["qemu"]
		assert.Equal(t, uint(512), qemuConfig.DefaultMemoryMB)

		// others should have default values
		hvtConfig := config.Monitors["hvt"]
		assert.Equal(t, uint(256), hvtConfig.DefaultMemoryMB)
		assert.Equal(t, uint(1), hvtConfig.DefaultVCPUs)
	})

	t.Run("single extra binary with all fields", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testVirtiofsdPathKey: testQemuBinaryPath,
			testVirtiofsdOptsKey: testBinOpts,
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.ExtraBins, "virtiofsd")
		vfsConfig := config.ExtraBins["virtiofsd"]
		assert.Equal(t, testQemuBinaryPath, vfsConfig.Path)
		assert.Equal(t, testBinOpts, vfsConfig.Options)
	})

	t.Run("multiple extra binaries", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testVirtiofsdPathKey:                         testQemuBinaryPath,
			testVirtiofsdOptsKey:                         testBinOpts,
			"urunc_config.extra_binaries.binary.path":    "/path/to/bin",
			"urunc_config.extra_binaries.binary.options": "some opts",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.ExtraBins, "virtiofsd")
		assert.Contains(t, config.ExtraBins, "binary")

		vfsConfig := config.ExtraBins["virtiofsd"]
		assert.Equal(t, testQemuBinaryPath, vfsConfig.Path)
		assert.Equal(t, testBinOpts, vfsConfig.Options)

		binConfig := config.ExtraBins["binary"]
		assert.Equal(t, "/path/to/bin", binConfig.Path)
		assert.Equal(t, "some opts", binConfig.Options)
	})

	t.Run("partial extra binary config", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testVirtiofsdPathKey: testQemuBinaryPath,
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.ExtraBins, "virtiofsd")
		vfsConfig := config.ExtraBins["virtiofsd"]
		assert.Equal(t, testQemuBinaryPath, vfsConfig.Path)
		assert.Equal(t, testVirtiofsdDefOpts, vfsConfig.Options)
	})

	t.Run("unknown extra binary config field is ignored", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testVirtiofsdPathKey: testQemuBinaryPath,
			"urunc_config.extra_binaries.virtiofsd.unknown_field": "value",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.ExtraBins, "virtiofsd")
		vfsConfig := config.ExtraBins["virtiofsd"]
		assert.Equal(t, testQemuBinaryPath, vfsConfig.Path)
		assert.Equal(t, testVirtiofsdDefOpts, vfsConfig.Options)
	})

	t.Run("new unknown extra binary config", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			"urunc_config.extra_binaries.custom.path":    "/custom/binary",
			"urunc_config.extra_binaries.custom.options": testBinOpts,
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.ExtraBins, "custom")
		cConfig := config.ExtraBins["custom"]
		assert.Equal(t, cfgMap["urunc_config.extra_binaries.custom.path"], cConfig.Path)
		assert.Equal(t, cfgMap["urunc_config.extra_binaries.custom.options"], cConfig.Options)
	})

	t.Run("preserves default extra binaries not in map", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			"urunc_config.extra_binaries.custom.path":    "/custom/binary",
			"urunc_config.extra_binaries.custom.options": testBinOpts,
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		// Should still contain all default extra binaries
		assert.Contains(t, config.ExtraBins, "virtiofsd")
		vfsConfig := config.ExtraBins["virtiofsd"]
		// Should have default values
		assert.Equal(t, "/usr/libexec/virtiofsd", vfsConfig.Path)
		assert.Equal(t, testVirtiofsdDefOpts, vfsConfig.Options)
	})

	t.Run("vhost false is parsed correctly", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey: "512",
			testQemuVhostKey:  "false",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		qemuConfig := config.Monitors["qemu"]
		assert.False(t, qemuConfig.Vhost)
	})

	t.Run("invalid vhost value defaults to false", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey: "512",
			testQemuVhostKey:  "invalid",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		qemuConfig := config.Monitors["qemu"]
		assert.False(t, qemuConfig.Vhost, "invalid vhost value should default to false")
	})

}

func TestUruncConfigMap(t *testing.T) {
	t.Run("default config produces expected map", func(t *testing.T) {
		t.Parallel()
		config := defaultUruncConfig()

		cfgMap := config.Map()

		assert.NotNil(t, cfgMap)

		// Check that all default monitors are in the map
		expectedKeys := []string{
			testQemuMemoryKey,
			testQemuVCPUsKey,
			testQemuBinaryKey,
			"urunc_config.monitors.hvt.default_memory_mb",
			"urunc_config.monitors.hvt.default_vcpus",
			"urunc_config.monitors.hvt.binary_path",
			"urunc_config.monitors.spt.default_memory_mb",
			"urunc_config.monitors.spt.default_vcpus",
			"urunc_config.monitors.spt.binary_path",
			"urunc_config.monitors.firecracker.default_memory_mb",
			"urunc_config.monitors.firecracker.default_vcpus",
			"urunc_config.monitors.firecracker.binary_path",
			testVirtiofsdPathKey,
			testVirtiofsdOptsKey,
		}

		for _, key := range expectedKeys {
			assert.Contains(t, cfgMap, key)
		}

		// Check default values
		assert.Equal(t, "256", cfgMap[testQemuMemoryKey])
		assert.Equal(t, "1", cfgMap[testQemuVCPUsKey])
		assert.Equal(t, "", cfgMap[testQemuBinaryKey])
		assert.Equal(t, "/usr/libexec/virtiofsd", cfgMap[testVirtiofsdPathKey])
		assert.Equal(t, testVirtiofsdDefOpts, cfgMap[testVirtiofsdOptsKey])
	})

	t.Run("custom config produces expected map", func(t *testing.T) {
		t.Parallel()
		config := &UruncConfig{
			Monitors: map[string]types.MonitorConfig{
				"custom": {
					DefaultMemoryMB: 2048,
					DefaultVCPUs:    4,
					BinaryPath:      "/custom/path",
				},
			},
			ExtraBins: map[string]types.ExtraBinConfig{
				"custom": {
					Path:    "/custom/path",
					Options: "some opts",
				},
			},
		}

		cfgMap := config.Map()

		assert.NotNil(t, cfgMap)
		assert.Equal(t, "2048", cfgMap["urunc_config.monitors.custom.default_memory_mb"])
		assert.Equal(t, "4", cfgMap["urunc_config.monitors.custom.default_vcpus"])
		assert.Equal(t, "/custom/path", cfgMap["urunc_config.monitors.custom.binary_path"])
		assert.Equal(t, config.ExtraBins["custom"].Path, cfgMap["urunc_config.extra_binaries.custom.path"])
		assert.Equal(t, config.ExtraBins["custom"].Options, cfgMap["urunc_config.extra_binaries.custom.options"])
	})

	t.Run("empty monitors map produces empty result", func(t *testing.T) {
		t.Parallel()
		config := &UruncConfig{
			Monitors: map[string]types.MonitorConfig{},
		}

		cfgMap := config.Map()

		assert.NotNil(t, cfgMap)
		assert.Empty(t, cfgMap)
	})

	t.Run("empty extra binaries map produces empty result", func(t *testing.T) {
		t.Parallel()
		config := &UruncConfig{
			ExtraBins: map[string]types.ExtraBinConfig{},
		}

		cfgMap := config.Map()

		assert.NotNil(t, cfgMap)
		assert.Empty(t, cfgMap)
	})

	t.Run("vhost true is serialized correctly", func(t *testing.T) {
		t.Parallel()
		config := &UruncConfig{
			Monitors: map[string]types.MonitorConfig{
				"qemu": {
					DefaultMemoryMB: 512,
					DefaultVCPUs:    2,
					Vhost:           true,
				},
			},
			ExtraBins: map[string]types.ExtraBinConfig{},
		}

		cfgMap := config.Map()

		assert.Equal(t, "true", cfgMap[testQemuVhostKey])
	})
}

func TestDefaultConfigs(t *testing.T) {
	t.Run("defaultLogConfig", func(t *testing.T) {
		t.Parallel()
		config := defaultLogConfig()

		assert.Equal(t, "info", config.Level)
		assert.False(t, config.Syslog)
	})

	t.Run("defaultTimestampsConfig", func(t *testing.T) {
		t.Parallel()
		config := defaultTimestampsConfig()

		assert.False(t, config.Enabled)
		assert.Equal(t, testTimestampsPath, config.Destination)
	})

	t.Run("defaultMonitorsConfig", func(t *testing.T) {
		t.Parallel()
		config := defaultMonitorsConfig()

		assert.Len(t, config, 5)
		assert.Contains(t, config, "qemu")
		assert.Contains(t, config, "hvt")
		assert.Contains(t, config, "spt")
		assert.Contains(t, config, "firecracker")
		assert.Contains(t, config, "cloud-hypervisor")

		// Check default values for each monitor
		for _, hvConfig := range config {
			assert.Equal(t, uint(256), hvConfig.DefaultMemoryMB)
			assert.Equal(t, uint(1), hvConfig.DefaultVCPUs)
			assert.Equal(t, "", hvConfig.BinaryPath)
			assert.False(t, hvConfig.Vhost, "vhost should be false by default")
		}
	})

	t.Run("defaultExtraBinConfig", func(t *testing.T) {
		t.Parallel()
		config := defaultExtraBinConfig()

		assert.Len(t, config, 1)
		assert.Contains(t, config, "virtiofsd")

		assert.Equal(t, "/usr/libexec/virtiofsd", config["virtiofsd"].Path)
		assert.Equal(t, testVirtiofsdDefOpts, config["virtiofsd"].Options)
	})

	t.Run("defaultUruncConfig", func(t *testing.T) {
		t.Parallel()
		config := defaultUruncConfig()

		assert.NotNil(t, config)
		assert.Equal(t, "info", config.Log.Level)
		assert.False(t, config.Log.Syslog)
		assert.False(t, config.Timestamps.Enabled)
		assert.Equal(t, testTimestampsPath, config.Timestamps.Destination)
		assert.Len(t, config.Monitors, 5)
		assert.Len(t, config.ExtraBins, 1)
	})

	t.Run("defaultLogMetricsConfig", func(t *testing.T) {
		t.Parallel()
		config := defaultLogMetricsConfig()

		assert.Equal(t, "info", config.Log.Level)
		assert.False(t, config.Log.Syslog)
		assert.False(t, config.Timestamps.Enabled)
		assert.Equal(t, testTimestampsPath, config.Timestamps.Destination)
	})
}
