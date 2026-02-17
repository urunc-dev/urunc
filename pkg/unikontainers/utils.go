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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urunc-dev/urunc/internal/constants"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const (
	configFilename    = "config.json"
	stateFilename     = "state.json"
	initPidFilename   = "init.pid"
	uruncJSONFilename = "urunc.json"
	rootfsDirName     = "rootfs"
)

// getInitPid extracts "init_process_pid" value from the given JSON file
func getInitPid(filePath string) (float64, error) {
	// Open the JSON file for reading
	file, err := os.Open(filePath)
	if err != nil {
		return 0, nil
	}
	defer file.Close()

	// Decode the JSON data into a map[string]interface{}
	var jsonData map[string]interface{}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&jsonData); err != nil {
		return 0, nil
	}

	// Extract the specific value "init_process_pid"
	initProcessPID, found := jsonData["init_process_pid"].(float64) // Assuming it's a numeric value
	if !found {
		return 0, nil
	}
	return initProcessPID, nil
}

// copy sourceFile to targetDir
// creates targetDir and all necessary parent directories
func copyFile(sourceFile string, targetPath string) error {
	targetDir := filepath.Dir(targetPath)
	source, err := os.Open(sourceFile)
	if err != nil {
		return err
	}
	defer source.Close()

	err = os.MkdirAll(targetDir, 0755)
	if err != nil {
		return err
	}

	target, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer target.Close()

	_, err = io.Copy(target, source)
	if err != nil {
		return err
	}

	return nil
}

// move sourceFile to targetDir
// creates targetDir and all necessary parent directories
func moveFile(sourceFile string, targetDir string) error {
	_, filename := filepath.Split(sourceFile)
	targetPath := filepath.Join(targetDir, filename)
	err := copyFile(sourceFile, targetPath)
	if err != nil {
		return err
	}
	return os.Remove(sourceFile)
}

// loadSpec returns the Spec found in the given bundle directory
func loadSpec(bundleDir string) (*specs.Spec, error) {
	var spec specs.Spec

	absBundleDir, err := filepath.Abs(bundleDir)
	if err != nil {
		return nil, fmt.Errorf("failed to find absolute path of bundle: %w", err)
	}

	configFile := filepath.Join(absBundleDir, configFilename)
	specData, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read specification file: %w", err)
	}

	if err := json.Unmarshal(specData, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse specification json: %w", err)
	}

	return &spec, nil
}

// writePidFile writes the content of pid to the file defined by path
func writePidFile(path string, pid int) error {
	var (
		tmpDir  = filepath.Dir(path)
		tmpName = filepath.Join(tmpDir, "."+filepath.Base(path))
	)
	f, err := os.OpenFile(tmpName, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0o666)
	if err != nil {
		return err
	}
	_, err = f.WriteString(strconv.Itoa(pid))
	f.Close()
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// handleQueueProxy adds a hardcoded IP to the process's environment.
// Then, the container is identified as a non-bima container
// is spawned using runc.
func handleQueueProxy(spec specs.Spec, configFile string) error {
	var readinessProbeEnv string
	for i, envVar := range spec.Process.Env {
		if strings.HasPrefix(envVar, "SERVING_READINESS_PROBE") {
			spec.Process.Env = remove(spec.Process.Env, i)
			re := regexp.MustCompile(`"host"\s*:\s*"[^"]+"`)
			readinessProbeEnv = re.ReplaceAllString(envVar, `"host":"`+constants.QueueProxyRedirectIP+`"`)
			break
		}
	}

	redirectIPEnv := fmt.Sprintf("REDIRECT_IP=%s", constants.QueueProxyRedirectIP)
	envs := []string{readinessProbeEnv, redirectIPEnv}
	spec.Process.Env = append(spec.Process.Env, envs...)

	// Get permissions of specification file
	fileInfo, err := os.Stat(configFile)
	if err != nil {
		return fmt.Errorf("error getting file info: %v", err)
	}
	permissions := fileInfo.Mode()

	// Write the modified struct back to the JSON file
	updatedData, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling JSON: %v", err)
	}

	err = os.WriteFile(configFile, updatedData, permissions)
	if err != nil {
		return fmt.Errorf("error writing to file: %v", err)
	}

	// Exec runc to handle the Queue Proxy container
	return nil
}

func remove(s []string, i int) []string {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

func checkValidNsPath(path string) error {
	// only set to join this namespace if it exists
	if _, err := os.Lstat(path); err != nil {
		return ErrNotExistingNS
	}
	// do not allow namespace path with comma as we use it to separate
	// the namespace paths
	if strings.ContainsRune(path, ',') {
		return fmt.Errorf("invalid namespace path %s", path)
	}

	return nil
}

func convertUint32ToIntSlice(valSlice []uint32, size int) []int {
	retSlice := make([]int, size)
	for i, val := range valSlice {
		retSlice[i] = int(val)
	}

	return retSlice
}

// TODO: Use it when we enable user namespaces
// func encodeIDMapping(idMap []specs.LinuxIDMapping) ([]byte, error) {
// 	data := bytes.NewBuffer(nil)
// 	for _, im := range idMap {
// 		line := fmt.Sprintf("%d %d %d\n", im.ContainerID, im.HostID, im.Size)
// 		if _, err := data.WriteString(line); err != nil {
// 			return nil, err
// 		}
// 	}
// 	return data.Bytes(), nil
// }

func spawnVirtiofsd(vfsdConf types.ExtraBinConfig, sharedPath string) error {
	args := []string{
		"--socket-path=/tmp/vhostqemu",
		"--shared-dir",
		sharedPath,
	}

	if vfsdConf.Options != "" {
		args = append(args, strings.Fields(vfsdConf.Options)...)
	}

	// #nosec G204 -- virtiofsd path and options come from the urunc configuration, which is considered trusted
	cmd := exec.Command(vfsdConf.Path, args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	return nil
}

func resolveAgainstBase(base string, path string) (string, error) {
	resolvedPath := path

	if !filepath.IsAbs(path) {
		baseAbs := base
		var err error

		if !filepath.IsAbs(base) {
			baseAbs, err = filepath.Abs(base)
			if err != nil {
				return "", fmt.Errorf("could not get absolute path of %s: %w", base, err)
			}
		}
		resolvedPath = filepath.Join(baseAbs, path)
	}

	return resolvedPath, nil
}

func fileExists(fpath string) bool {
	var fileInfo unix.Stat_t

	err := unix.Stat(fpath, &fileInfo)
	if err != nil {
		uniklog.Infof("Stat %s failed with: %v", fpath, err)
		return false
	}

	return true
}

// containsNS checks of the container's configuration contains a specific namespace
func findNS(namespaces []specs.LinuxNamespace, nsType specs.LinuxNamespaceType) (string, error) {
	for _, ns := range namespaces {
		if ns.Type == nsType {
			err := checkValidNsPath(ns.Path)
			if err != nil {
				return "", err
			}
			return ns.Path, nil
		}
	}

	return "", fmt.Errorf("Namespace %s was not found", string(nsType))
}

// findQemuDataDir tries to find the location of data and BIOS files for Qemu.
// At first checks /usr/local/share and if it does not exist, it falls back to
// /usr/share. If /usr/local/share is a soft link, it will find its target.
func findQemuDataDir(basename string) (string, error) {
	// First check if the file exists under /usr/local/share
	qdPath := filepath.Join("/usr/local/share/", basename)
	info, err := os.Lstat(qdPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to get info of %s: %w", qdPath, err)
		}
		// The file does not exist under /usr/local/share
		// fallback to the usual path /usr/share/
		qdPath = filepath.Join("/usr/share/", basename)
	} else {
		// The file exists under /usr/local/share, but check if it is a link
		if info.Mode()&os.ModeSymlink != 0 {
			// It is a link, get the target
			qdPath, err = os.Readlink(qdPath)
			if err != nil {
				return "", fmt.Errorf("failed to get target of %s %w", qdPath, err)
			}
		}

		// It is not a link, so we found it
		return qdPath, nil
	}

	return qdPath, nil
}

func rmMultipleDirs(prefixPath string, dirs []string) error {
	for _, d := range dirs {
		path := filepath.Join(prefixPath, d)
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("cannot remove %s: %w", d, err)
		}
	}

	return nil
}

func executeHook(hook specs.Hook, state []byte) error {
	var stdout, stderr bytes.Buffer
	var cancel context.CancelFunc
	ctx := context.Background()

	// Apply hook-specific timeout if set, otherwise use global config timeout
	if hook.Timeout != nil && *hook.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*hook.Timeout)*time.Second)
	}
	if cancel != nil {
		defer cancel()
	}

	// Skip the first argument (the binary name) because CommandContext() adds
	// the binary path as the first arg automatically.
	args := hook.Args
	if len(args) > 0 {
		args = args[1:]
	}
	// Hook path and args come from OCI runtime spec configuration.
	// We are expected to execute whatever is there.
	cmd := exec.CommandContext(ctx, hook.Path, args...) // nolint:gosec
	cmd.Env = hook.Env
	cmd.Stdin = bytes.NewReader(state)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed hook: %s stdout: %s stderr %s error: %w", hook.Path, stdout.String(), stderr.String(), err)
	}

	return nil
}
