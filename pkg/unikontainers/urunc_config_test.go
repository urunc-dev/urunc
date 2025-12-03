// Copyright (c) 2023-2025, Nubificus LTD
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
	testQemuMemoryKey    = "urunc_config.hypervisors.qemu.default_memory_mb"
	testQemuVCPUsKey     = "urunc_config.hypervisors.qemu.default_vcpus"
	testQemuBinaryKey    = "urunc_config.hypervisors.qemu.binary_path"
	testHvtMemoryKey     = "urunc_config.hypervisors.hvt.default_memory_mb"
	testVirtiofsdPathKey = "urunc_config.extra_binaries.virtiofsd.path"
	testVirtiofsdOptsKey = "urunc_config.extra_binaries.virtiofsd.options"
	testVirtiofsdDefOpts = "--cache always --sandbox none"
	testBinOpts          = "opt1 opt2"
	testQemuBinaryPath   = "/usr/bin/qemu"
	testTimestampsPath   = "/var/log/urunc/timestamps.log"
)

func TestUruncConfigFromMap(t *testing.T) {
	t.Run("empty map returns default config", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Equal(t, defaultHypervisorsConfig(), config.Hypervisors)
		assert.Equal(t, defaultExtraBinConfig(), config.ExtraBins)
	})

	t.Run("single hypervisor with all fields", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey: "512",
			testQemuVCPUsKey:  "2",
			testQemuBinaryKey: testQemuBinaryPath,
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.Hypervisors, "qemu")
		qemuConfig := config.Hypervisors["qemu"]
		assert.Equal(t, uint(512), qemuConfig.DefaultMemoryMB)
		assert.Equal(t, uint(2), qemuConfig.DefaultVCPUs)
		assert.Equal(t, testQemuBinaryPath, qemuConfig.BinaryPath)
	})

	t.Run("multiple hypervisors", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey: "512",
			testQemuVCPUsKey:  "2",
			"urunc_config.hypervisors.firecracker.default_memory_mb": "128",
			"urunc_config.hypervisors.firecracker.binary_path":       "/usr/bin/firecracker",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.Hypervisors, "qemu")
		assert.Contains(t, config.Hypervisors, "firecracker")

		qemuConfig := config.Hypervisors["qemu"]
		assert.Equal(t, uint(512), qemuConfig.DefaultMemoryMB)
		assert.Equal(t, uint(2), qemuConfig.DefaultVCPUs)

		firecrackerConfig := config.Hypervisors["firecracker"]
		assert.Equal(t, uint(128), firecrackerConfig.DefaultMemoryMB)
		assert.Equal(t, "/usr/bin/firecracker", firecrackerConfig.BinaryPath)
	})

	t.Run("partial hypervisor config", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testHvtMemoryKey: "1024",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.Hypervisors, "hvt")
		hvtConfig := config.Hypervisors["hvt"]
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
			"urunc_config.hypervisors.qemu.field.extra.parts": "invalid",
			testHvtMemoryKey: "512",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.Hypervisors, "qemu")
		qemuConfig := config.Hypervisors["qemu"]
		assert.Equal(t, uint(256), qemuConfig.DefaultMemoryMB) // Default value for invalid input
		assert.Equal(t, uint(1), qemuConfig.DefaultVCPUs)      // Default value for negative input
		assert.Equal(t, testQemuBinaryPath, qemuConfig.BinaryPath)
		assert.Contains(t, config.Hypervisors, "hvt")
		hvtConfig := config.Hypervisors["hvt"]
		assert.Equal(t, uint(512), hvtConfig.DefaultMemoryMB)
	})

	t.Run("unknown hypervisor field is ignored", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			"urunc_config.hypervisors.qemu.unknown_field": "value",
			testQemuMemoryKey: "512",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		qemuConfig := config.Hypervisors["qemu"]
		assert.Equal(t, uint(512), qemuConfig.DefaultMemoryMB)
	})

	t.Run("new hypervisor not in default config", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			"urunc_config.hypervisors.custom.default_memory_mb": "2048",
			"urunc_config.hypervisors.custom.default_vcpus":     "4",
			"urunc_config.hypervisors.custom.binary_path":       "/custom/hypervisor",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		assert.Contains(t, config.Hypervisors, "custom")
		customConfig := config.Hypervisors["custom"]
		assert.Equal(t, uint(2048), customConfig.DefaultMemoryMB)
		assert.Equal(t, uint(4), customConfig.DefaultVCPUs)
		assert.Equal(t, "/custom/hypervisor", customConfig.BinaryPath)
	})

	t.Run("mixed valid and invalid entries", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey:                            "512",
			"invalid.key.format":                         "ignored",
			"urunc_config.hypervisors.hvt.default_vcpus": "invalid_number",
			"urunc_config.hypervisors.spt.binary_path":   "/usr/bin/spt",
			"urunc_config.hypervisors":                   "malformed",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)

		// qemu should have memory set
		qemuConfig := config.Hypervisors["qemu"]
		assert.Equal(t, uint(512), qemuConfig.DefaultMemoryMB)

		// hvt should preserve default vcpus value due to invalid input
		hvtConfig := config.Hypervisors["hvt"]
		assert.Equal(t, uint(1), hvtConfig.DefaultVCPUs)

		// spt should have binary path set
		sptConfig := config.Hypervisors["spt"]
		assert.Equal(t, "/usr/bin/spt", sptConfig.BinaryPath)
	})

	t.Run("preserves default hypervisors not in map", func(t *testing.T) {
		t.Parallel()
		cfgMap := map[string]string{
			testQemuMemoryKey: "512",
		}

		config := UruncConfigFromMap(cfgMap)

		assert.NotNil(t, config)
		// Should still contain all default hypervisors
		assert.Contains(t, config.Hypervisors, "qemu")
		assert.Contains(t, config.Hypervisors, "hvt")
		assert.Contains(t, config.Hypervisors, "spt")
		assert.Contains(t, config.Hypervisors, "firecracker")

		// qemu should be modified
		qemuConfig := config.Hypervisors["qemu"]
		assert.Equal(t, uint(512), qemuConfig.DefaultMemoryMB)

		// others should have default values
		hvtConfig := config.Hypervisors["hvt"]
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
			testVirtiofsdPathKey: testQemuBinaryPath,
			testVirtiofsdOptsKey: testBinOpts,
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

}

func TestUruncConfigMap(t *testing.T) {
	t.Run("default config produces expected map", func(t *testing.T) {
		t.Parallel()
		config := defaultUruncConfig()

		cfgMap := config.Map()

		assert.NotNil(t, cfgMap)

		// Check that all default hypervisors are in the map
		expectedKeys := []string{
			testQemuMemoryKey,
			testQemuVCPUsKey,
			testQemuBinaryKey,
			"urunc_config.hypervisors.hvt.default_memory_mb",
			"urunc_config.hypervisors.hvt.default_vcpus",
			"urunc_config.hypervisors.hvt.binary_path",
			"urunc_config.hypervisors.spt.default_memory_mb",
			"urunc_config.hypervisors.spt.default_vcpus",
			"urunc_config.hypervisors.spt.binary_path",
			"urunc_config.hypervisors.firecracker.default_memory_mb",
			"urunc_config.hypervisors.firecracker.default_vcpus",
			"urunc_config.hypervisors.firecracker.binary_path",
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
			Hypervisors: map[string]types.HypervisorConfig{
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
		assert.Equal(t, "2048", cfgMap["urunc_config.hypervisors.custom.default_memory_mb"])
		assert.Equal(t, "4", cfgMap["urunc_config.hypervisors.custom.default_vcpus"])
		assert.Equal(t, "/custom/path", cfgMap["urunc_config.hypervisors.custom.binary_path"])
		assert.Equal(t, config.ExtraBins["custom"].Path, cfgMap["urunc_config.extra_binaries.custom.path"])
		assert.Equal(t, config.ExtraBins["custom"].Options, cfgMap["urunc_config.extra_binaries.custom.options"])
	})

	t.Run("empty hypervisors map produces empty result", func(t *testing.T) {
		t.Parallel()
		config := &UruncConfig{
			Hypervisors: map[string]types.HypervisorConfig{},
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

	t.Run("defaultHypervisorsConfig", func(t *testing.T) {
		t.Parallel()
		config := defaultHypervisorsConfig()

		assert.Len(t, config, 4)
		assert.Contains(t, config, "qemu")
		assert.Contains(t, config, "hvt")
		assert.Contains(t, config, "spt")
		assert.Contains(t, config, "firecracker")

		// Check default values for each hypervisor
		for _, hvConfig := range config {
			assert.Equal(t, uint(256), hvConfig.DefaultMemoryMB)
			assert.Equal(t, uint(1), hvConfig.DefaultVCPUs)
			assert.Equal(t, "", hvConfig.BinaryPath)
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
		assert.Len(t, config.Hypervisors, 4)
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
