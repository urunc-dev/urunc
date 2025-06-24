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
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
)

func TestGetConfigFromSpec(t *testing.T) {
	t.Run("get config from spec success", func(t *testing.T) {
		t.Parallel()
		spec := &specs.Spec{
			Annotations: map[string]string{
				annotType:          "type1",
				annotCmdLine:       "cmd1",
				annotBinary:        "binary1",
				annotHypervisor:    "hypervisor1",
				annotInitrd:        "initrd1",
				annotBlock:         "block1",
				annotBlockMntPoint: "point1",
				annotMountRootfs:   "true",
			},
		}

		expectedConfig := &UnikernelConfig{
			UnikernelBinary: "binary1",
			UnikernelType:   "type1",
			UnikernelCmd:    "cmd1",
			Hypervisor:      "hypervisor1",
			Initrd:          "initrd1",
			Block:           "block1",
			BlkMntPoint:     "point1",
			MountRootfs:     "true",
		}

		config, err := getConfigFromSpec(spec)
		assert.NoError(t, err, "Expected no error")
		assert.Equal(t, expectedConfig, config, "Expected config to match")
	})

	t.Run("get config from spec empty annotations", func(t *testing.T) {
		t.Parallel()
		spec := &specs.Spec{
			Annotations: map[string]string{},
		}

		config, err := getConfigFromSpec(spec)
		assert.Error(t, err, "Expected an error")
		assert.Nil(t, config, "Expected config to be nil")
		assert.Equal(t, ErrEmptyAnnotations, err, "Expected ErrEmptyAnnotations")
	})

	t.Run("get config from spec partial annotations", func(t *testing.T) {
		t.Parallel()
		spec := &specs.Spec{
			Annotations: map[string]string{
				annotType: "type1",
			},
		}

		expectedConfig := &UnikernelConfig{
			UnikernelType: "type1",
		}

		config, err := getConfigFromSpec(spec)
		assert.NoError(t, err, "Expected no error")
		assert.Equal(t, expectedConfig, config, "Expected partial config to match")
	})
}

func TestGetConfigFromJSON(t *testing.T) {
	t.Run("get config from json success", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()

		// Create a valid urunc.json file
		expectedConfig := &UnikernelConfig{
			UnikernelBinary: "binary1",
			UnikernelType:   "type1",
			UnikernelCmd:    "cmd1",
			Hypervisor:      "hypervisor1",
			Initrd:          "initrd1",
			Block:           "block1",
			BlkMntPoint:     "point1",
			MountRootfs:     "true",
		}
		configData, err := json.Marshal(expectedConfig)
		assert.NoError(t, err)

		rootfsDir := filepath.Join(tempDir, rootfsDirName)
		err = os.Mkdir(rootfsDir, 0755)
		assert.NoError(t, err)

		configPath := filepath.Join(rootfsDir, uruncJSONFilename)
		err = os.WriteFile(configPath, configData, 0600)
		assert.NoError(t, err)

		// Call the function
		config, err := getConfigFromJSON(configPath)
		assert.NoError(t, err, "Expected no error in getting config from JSON")
		assert.Equal(t, expectedConfig, config, "Expected config to match")
	})

	t.Run("get config from json file not found", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, uruncJSONFilename)

		// Call the function with a missing urunc.json file
		_, err := getConfigFromJSON(configPath)
		assert.Error(t, err, "Expected an error for missing "+
			uruncJSONFilename+" file")
		assert.Contains(t, err.Error(), "no such file or directory", "Expected specific error message")
	})

	t.Run("get config from json is directory", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()

		// Create a directory instead of a urunc.json file
		rootfsDir := filepath.Join(tempDir, rootfsDirName)
		err := os.Mkdir(rootfsDir, 0755)
		assert.NoError(t, err)
		configDirPath := filepath.Join(rootfsDir, uruncJSONFilename)
		err = os.Mkdir(configDirPath, 0755)
		assert.NoError(t, err)

		// Call the function
		_, err = getConfigFromJSON(configDirPath)
		assert.Error(t, err, "Expected an error for "+uruncJSONFilename+" being a directory")
		assert.Contains(t, err.Error(), uruncJSONFilename+" is a directory", "Expected specific error message")
	})

	t.Run("get config from invalid JSON", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()

		// Create an invalid urunc.json file
		rootfsDir := filepath.Join(tempDir, rootfsDirName)
		err := os.Mkdir(rootfsDir, 0755)
		assert.NoError(t, err)

		configPath := filepath.Join(rootfsDir, uruncJSONFilename)
		err = os.WriteFile(configPath, []byte("invalid json"), 0600)
		assert.NoError(t, err)

		// Call the function
		_, err = getConfigFromJSON(configPath)
		assert.Error(t, err, "Expected an error for invalid "+uruncJSONFilename+" file")
		assert.Contains(t, err.Error(), "invalid character", "Expected specific error message")
	})
}

func TestDecode(t *testing.T) {
	t.Run("decode success", func(t *testing.T) {
		t.Parallel()
		// Prepare the encoded values
		encodedCmd := base64.StdEncoding.EncodeToString([]byte("testCmd"))
		encodedHypervisor := base64.StdEncoding.EncodeToString([]byte("testHypervisor"))
		encodedType := base64.StdEncoding.EncodeToString([]byte("testType"))
		encodedBinary := base64.StdEncoding.EncodeToString([]byte("testBinary"))
		encodedInitrd := base64.StdEncoding.EncodeToString([]byte("testInitrd"))

		config := &UnikernelConfig{
			UnikernelCmd:    encodedCmd,
			Hypervisor:      encodedHypervisor,
			UnikernelType:   encodedType,
			UnikernelBinary: encodedBinary,
			Initrd:          encodedInitrd,
		}

		// Call the decode method
		err := config.decode()

		// Assert that no error occurred and the values are decoded correctly
		assert.NoError(t, err)
		assert.Equal(t, "testCmd", config.UnikernelCmd)
		assert.Equal(t, "testHypervisor", config.Hypervisor)
		assert.Equal(t, "testType", config.UnikernelType)
		assert.Equal(t, "testBinary", config.UnikernelBinary)
		assert.Equal(t, "testInitrd", config.Initrd)
	})

	t.Run("decode invalid base64", func(t *testing.T) {
		t.Parallel()
		// Prepare invalid base64 values
		invalidBase64 := "invalid-base64"

		config := &UnikernelConfig{
			UnikernelCmd:    invalidBase64,
			Hypervisor:      invalidBase64,
			UnikernelType:   invalidBase64,
			UnikernelBinary: invalidBase64,
			Initrd:          invalidBase64,
		}
		// Call the decode method and expect an error
		err := config.decode()

		// Assert that an error occurred
		assert.Error(t, err)
	})
}

func TestMap(t *testing.T) {
	t.Run("unikernelConfig map success", func(t *testing.T) {
		t.Parallel()
		config := &UnikernelConfig{
			UnikernelBinary: "binary_value",
			UnikernelType:   "type_value",
			UnikernelCmd:    "cmd_value",
			Hypervisor:      "hypervisor_value",
			Initrd:          "initrd_value",
			Block:           "block_value",
			BlkMntPoint:     "point_value",
			MountRootfs:     "false",
		}
		expectedMap := map[string]string{
			annotCmdLine:       "cmd_value",
			annotType:          "type_value",
			annotHypervisor:    "hypervisor_value",
			annotBinary:        "binary_value",
			annotInitrd:        "initrd_value",
			annotBlock:         "block_value",
			annotBlockMntPoint: "point_value",
			annotMountRootfs:   "false",
		}
		resultMap := config.Map()
		assert.Equal(t, expectedMap, resultMap)
	})
	t.Run("unikernelConfig map empty fields", func(t *testing.T) {
		t.Parallel()
		config := &UnikernelConfig{
			UnikernelBinary: "",
			UnikernelType:   "",
			UnikernelCmd:    "",
			Hypervisor:      "",
			Initrd:          "",
			Block:           "",
			BlkMntPoint:     "",
			MountRootfs:     "",
		}
		expectedMap := map[string]string{}
		resultMap := config.Map()
		assert.Equal(t, expectedMap, resultMap)
	})
	t.Run("unikernelConfig map partial fields", func(t *testing.T) {
		t.Parallel()
		config := &UnikernelConfig{
			UnikernelBinary: "binary_value",
			UnikernelType:   "",
			UnikernelCmd:    "cmd_value",
			Hypervisor:      "",
			Initrd:          "initrd_value",
			Block:           "",
			BlkMntPoint:     "point_value",
			MountRootfs:     "0",
		}
		expectedMap := map[string]string{
			annotCmdLine:       "cmd_value",
			annotBinary:        "binary_value",
			annotInitrd:        "initrd_value",
			annotBlockMntPoint: "point_value",
			annotMountRootfs:   "0",
		}
		resultMap := config.Map()
		assert.Equal(t, expectedMap, resultMap)
	})

	t.Run("unikernelConfig map no fields", func(t *testing.T) {
		t.Parallel()
		config := &UnikernelConfig{}
		expectedMap := map[string]string{}
		resultMap := config.Map()
		assert.Equal(t, expectedMap, resultMap)
	})
}
