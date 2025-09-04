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
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
)

func TestWritePidFile(t *testing.T) {
	tmpDir := t.TempDir() // Create a temporary directory for the test
	pidFilePath := filepath.Join(tmpDir, "test.pid")
	pid := 12345

	// Call the function
	err := writePidFile(pidFilePath, pid)
	assert.NoError(t, err, "Expected no error in writing PID file")

	// Check if the PID file exists
	_, err = os.Stat(pidFilePath)
	assert.NoError(t, err, "Expected PID file to exist")

	// Check if the content of the PID file is correct
	content, err := os.ReadFile(pidFilePath)
	assert.NoError(t, err, "Expected no error in reading PID file")
	assert.Equal(t, strconv.Itoa(pid), string(content), "Expected PID file content to be %d", pid)

	// Clean up
	os.Remove(pidFilePath)
}

func TestGetInitPid(t *testing.T) {
	t.Run("init PID found", func(t *testing.T) {
		t.Parallel()

		// Create a temporary file for testing
		tmpDir := t.TempDir()
		tmpFile, err := os.CreateTemp(tmpDir, "test*.json")
		assert.NoError(t, err)

		// Write test data to the file
		testData := map[string]interface{}{
			"init_process_pid": 12345.0,
		}
		jsonData, err := json.Marshal(testData)
		assert.NoError(t, err)

		_, err = tmpFile.Write(jsonData)
		assert.NoError(t, err)
		tmpFile.Close()

		// Call the function and check the result
		pid, err := getInitPid(tmpFile.Name())
		assert.NoError(t, err, "Expected no error in getting init PID")
		assert.Equal(t, 12345.0, pid, "Expected PID to be 12345")
	})
	t.Run("init PID file not found", func(t *testing.T) {
		t.Parallel()
		// Call the function with a non-existent file
		pid, err := getInitPid("nonexistent.json")
		assert.Equal(t, float64(0), pid, "Expected PID to be 0 for nonexistent file")
		assert.NoError(t, err, "Expected no error for nonexistent file")
	})

	t.Run("init PID invalid JSON", func(t *testing.T) {
		t.Parallel()
		// Create a temporary file with invalid JSON
		tmpDir := t.TempDir()
		tmpFile, err := os.CreateTemp(tmpDir, "test*.json")
		assert.NoError(t, err)
		_, err = tmpFile.WriteString("{invalid json}")
		assert.NoError(t, err)
		tmpFile.Close()

		// Call the function and check the result
		pid, err := getInitPid(tmpFile.Name())
		assert.Equal(t, float64(0), pid, "Expected PID to be 0 for invalid JSON")
		assert.NoError(t, err, "Expected no error for invalid JSON")
	})
	t.Run("init PID missing key", func(t *testing.T) {
		t.Parallel()
		// Create a temporary file without "init_process_pid"
		tmpDir := t.TempDir()
		tmpFile, err := os.CreateTemp(tmpDir, "test*.json")
		assert.NoError(t, err)

		testData := map[string]interface{}{
			"some_other_key": 12345.0,
		}
		jsonData, err := json.Marshal(testData)
		assert.NoError(t, err)

		_, err = tmpFile.Write(jsonData)
		assert.NoError(t, err)
		tmpFile.Close()

		// Call the function and check the result
		pid, err := getInitPid(tmpFile.Name())
		assert.Equal(t, float64(0), pid, "Expected PID to be 0 for missing key")
		assert.NoError(t, err, "Expected no error for missing key")
	})
}

func TestCopyFile(t *testing.T) {
	t.Run("copy file success", func(t *testing.T) {
		t.Parallel()
		// Create a temporary source file
		tmpDir := t.TempDir()
		srcFile, err := os.CreateTemp(tmpDir, "src*.txt")

		assert.NoError(t, err)

		// Write some content to the source file
		content := "Hello, world!"
		_, err = srcFile.WriteString(content)
		assert.NoError(t, err)
		srcFile.Close()

		// Create a temporary target directory
		targetDir := t.TempDir()
		_, filename := filepath.Split(srcFile.Name())
		copiedFilePath := filepath.Join(targetDir, filename)


		// Call the function
		err = copyFile(srcFile.Name(), copiedFilePath)
		assert.NoError(t, err, "Expected no error in copying file")

		// Verify the file was copied
		copiedContent, err := os.ReadFile(copiedFilePath)
		assert.NoError(t, err, "Expected no error in reading copied file")
		assert.Equal(t, content, string(copiedContent), "Expected copied content to match original")
	})

	t.Run("copy file no source found", func(t *testing.T) {
		t.Parallel()
		// Create a temporary target directory
		targetDir := t.TempDir()

		// Call the function with a non-existent source file
		err := copyFile("nonexistent.txt", targetDir)
		assert.Error(t, err, "Expected an error for non-existent source file")
	})

	t.Run("copy file target dir creation failed", func(t *testing.T) {
		t.Parallel()
		// Create a temporary source file
		tmpDir := t.TempDir()
		srcFile, err := os.CreateTemp(tmpDir, "src*.txt")
		assert.NoError(t, err)

		// Write some content to the source file
		content := "Hello, world!"
		_, err = srcFile.WriteString(content)
		assert.NoError(t, err)
		srcFile.Close()

		// Use a target directory path that cannot be created
		targetDir := filepath.Join(string(filepath.Separator), "invalid", "path")

		// Call the function
		err = copyFile(srcFile.Name(), targetDir)
		assert.Error(t, err, "Expected an error for invalid target directory path")
	})

	t.Run("copy file target file creation failed", func(t *testing.T) {
		t.Parallel()
		// Create a temporary source file
		tmpDir := t.TempDir()
		srcFile, err := os.CreateTemp(tmpDir, "src*.txt")
		assert.NoError(t, err)

		// Write some content to the source file
		content := "Hello, world!"
		_, err = srcFile.WriteString(content)
		assert.NoError(t, err)
		srcFile.Close()

		// Create a temporary target directory and a read-only file with the same name as the source file
		targetDir := t.TempDir()
		_, filename := filepath.Split(srcFile.Name())
		targetFilePath := filepath.Join(targetDir, filename)
		targetFile, err := os.OpenFile(targetFilePath, os.O_RDONLY|os.O_CREATE, 0444)
		assert.NoError(t, err)
		targetFile.Close()

		// Call the function
		err = copyFile(srcFile.Name(), targetFilePath)
		assert.Error(t, err, "Expected an error for read-only target file")
	})
}

func TestMoveFile(t *testing.T) {
	t.Run("move file success", func(t *testing.T) {
		t.Parallel()
		// Create a temporary source file
		tmpDir := t.TempDir()
		srcFile, err := os.CreateTemp(tmpDir, "src*.txt")
		assert.NoError(t, err)

		// Write some content to the source file
		content := "Hello, world!"
		_, err = srcFile.WriteString(content)
		assert.NoError(t, err)
		srcFile.Close()

		// Create a temporary target directory
		targetDir := t.TempDir()

		// Call the function
		err = moveFile(srcFile.Name(), targetDir)
		assert.NoError(t, err, "Expected no error in moving file")

		// Verify the file was moved
		_, filename := filepath.Split(srcFile.Name())
		movedFilePath := filepath.Join(targetDir, filename)
		movedContent, err := os.ReadFile(movedFilePath)
		assert.NoError(t, err, "Expected no error in reading moved file")
		assert.Equal(t, content, string(movedContent), "Expected moved content to match original")

		// Verify the source file was removed
		_, err = os.Stat(srcFile.Name())
		assert.True(t, os.IsNotExist(err), "Expected source file to be removed")
	})

	t.Run("move file source not found", func(t *testing.T) {
		t.Parallel()
		// Create a temporary target directory
		targetDir := t.TempDir()

		// Call the function with a non-existent source file
		err := moveFile("nonexistent.txt", targetDir)
		assert.Error(t, err, "Expected an error for non-existent source file")
	})

	t.Run("move file target dir creation failed", func(t *testing.T) {
		t.Parallel()
		// Create a temporary source file
		tmpDir := t.TempDir()
		srcFile, err := os.CreateTemp(tmpDir, "src*.txt")
		assert.NoError(t, err)

		// Write some content to the source file
		content := "Hello, world!"
		_, err = srcFile.WriteString(content)
		assert.NoError(t, err)
		srcFile.Close()

		// Use a target directory path that cannot be created
		targetDir := filepath.Join(string(filepath.Separator), "invalid", "path")

		// Call the function
		err = moveFile(srcFile.Name(), targetDir)
		assert.Error(t, err, "Expected an error for invalid target directory path")

		// Verify the source file still exists
		_, err = os.Stat(srcFile.Name())
		assert.False(t, os.IsNotExist(err), "Expected source file to still exist")
	})

	t.Run("move file target file creation failed", func(t *testing.T) {
		t.Parallel()
		// Create a temporary source file
		tmpDir := t.TempDir()
		srcFile, err := os.CreateTemp(tmpDir, "src*.txt")
		assert.NoError(t, err)

		// Write some content to the source file
		content := "Hello, world!"
		_, err = srcFile.WriteString(content)
		assert.NoError(t, err)
		srcFile.Close()

		// Create a temporary target directory and a read-only file with the same name as the source file
		targetDir := t.TempDir()
		_, filename := filepath.Split(srcFile.Name())
		targetFilePath := filepath.Join(targetDir, filename)
		targetFile, err := os.OpenFile(targetFilePath, os.O_RDONLY|os.O_CREATE, 0444)
		assert.NoError(t, err)
		targetFile.Close()

		// Call the function
		err = moveFile(srcFile.Name(), targetDir)
		assert.Error(t, err, "Expected an error for read-only target file")

		// Verify the source file still exists
		_, err = os.Stat(srcFile.Name())
		assert.False(t, os.IsNotExist(err), "Expected source file to still exist")
	})
}

func TestLoadSpec(t *testing.T) {
	t.Run("load spec success", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()

		// Create a valid config.json file
		spec := specs.Spec{
			Version: "1.0.0",
		}
		configData, err := json.Marshal(spec)
		assert.NoError(t, err)

		configPath := filepath.Join(tempDir, configFilename)
		err = os.WriteFile(configPath, configData, 0600)
		assert.NoError(t, err)

		// Call the function
		loadedSpec, err := loadSpec(tempDir)
		assert.NoError(t, err, "Expected no error in loading spec")
		assert.Equal(t, spec, *loadedSpec, "Expected loaded spec to match original")
	})

	t.Run("load spec invalid bundle path", func(t *testing.T) {
		t.Parallel()
		// Call the function with an invalid bundle path
		_, err := loadSpec("invalid/path")
		assert.Error(t, err, "Expected an error for invalid bundle path")
		assert.Contains(t, err.Error(), "no such file or directory", "Expected specific error message")
	})

	t.Run("load spec config file not found", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()

		// Call the function with a valid bundle path but without config.json
		_, err := loadSpec(tempDir)
		assert.Error(t, err, "Expected an error for missing "+configFilename+" file")
		assert.Contains(t, err.Error(), "failed to read specification file", "Expected specific error message")
	})

	t.Run("load spec invalid config file", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()

		// Create an invalid config.json file
		configPath := filepath.Join(tempDir, configFilename)
		err := os.WriteFile(configPath, []byte("invalid json"), 0600)
		assert.NoError(t, err)

		// Call the function
		_, err = loadSpec(tempDir)
		assert.Error(t, err, "Expected an error for invalid "+configFilename+" file")
		assert.Contains(t, err.Error(), "failed to parse specification json", "Expected specific error message")
	})
}
