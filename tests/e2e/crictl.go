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

package urunce2etesting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	criruntimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const crictlName = "crictl"
const podConfigFilename = "pod.json"
const cntrConfigFilename = "container.json"

type crictlInfo struct {
	testArgs    containerTestArgs
	podID       string
	containerID string
}

func newCrictlTool(args containerTestArgs) *crictlInfo {
	return &crictlInfo{
		testArgs:    args,
		podID:       "",
		containerID: "",
	}
}

func writeToFile(filename string, content string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	if err != nil {
		return err
	}
	return nil
}

func crictlNewPodConfig(path string, name string) (string, error) {
	podConfig := criruntimeapi.PodSandboxConfig{
		Metadata: &criruntimeapi.PodSandboxMetadata{
			Name:      name,
			Uid:       "abcshd83djaidwnduwk28bcsb",
			Namespace: "default",
			Attempt:   1,
		},
	}
	podConfigJSON, err := json.MarshalIndent(&podConfig, "", "    ")
	if err != nil {
		return "", fmt.Errorf("Failed to marshal pod config: %v", err)
	}
	absPodConf := filepath.Join(path, podConfigFilename)
	err = writeToFile(absPodConf, string(podConfigJSON))
	if err != nil {
		return "", fmt.Errorf("Failed to write pod config: %v", err)
	}
	return absPodConf, nil
}

func crictlNewContainerConfig(path string, a containerTestArgs) (string, error) {
	var name string
	if a.StaticNet {
		name = "user-container"
	} else {
		name = a.Name
	}
	var mounts []*criruntimeapi.Mount
	for _, vol := range a.Volumes {
		mounts = append(mounts, &criruntimeapi.Mount{
			ContainerPath: vol.Dest,
			HostPath:      vol.Source,
			Readonly:      false,
		})
	}
	containerConfig := criruntimeapi.ContainerConfig{
		Metadata: &criruntimeapi.ContainerMetadata{
			Name: name,
		},
		Image: &criruntimeapi.ImageSpec{
			Image: a.Image,
		},
		Command: strings.Fields(a.Cli),
		Mounts:  mounts,
		Linux: &criruntimeapi.LinuxContainerConfig{
			SecurityContext: &criruntimeapi.LinuxContainerSecurityContext{},
			Resources:       &criruntimeapi.LinuxContainerResources{},
		},
	}
	if a.Memory != "" {
		mem, err := strconv.ParseInt(a.Memory, 10, 64)
		if err != nil {
			return "", err
		}
		containerConfig.Linux.Resources.MemoryLimitInBytes = mem
	}
	if a.UID != 0 && a.GID != 0 {
		containerConfig.Linux.SecurityContext.RunAsUser = &criruntimeapi.Int64Value{Value: int64(a.UID)}
		containerConfig.Linux.SecurityContext.RunAsGroup = &criruntimeapi.Int64Value{Value: int64(a.GID)}
	}
	if len(a.Groups) != 0 {
		containerConfig.Linux.SecurityContext.SupplementalGroups = a.Groups
	}
	cc, err := json.MarshalIndent(&containerConfig, "", "    ")
	if err != nil {
		return "", fmt.Errorf("Failed to marshal container config: %v", err)
	}
	absContConf := filepath.Join(path, cntrConfigFilename)
	err = writeToFile(absContConf, string(cc))
	if err != nil {
		return "", fmt.Errorf("Failed to write container config: %v", err)
	}

	return absContConf, nil
}

func (i *crictlInfo) Name() string {
	return crictlName
}

func (i *crictlInfo) getTestArgs() containerTestArgs {
	return i.testArgs
}

func (i *crictlInfo) getPodID() string {
	return i.podID
}

func (i *crictlInfo) getContainerID() string {
	return i.containerID
}

func (i *crictlInfo) setPodID(pID string) {
	i.podID = pID
}

func (i *crictlInfo) setContainerID(cID string) {
	i.containerID = cID
}

func (i *crictlInfo) createPod() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("Failed to get CWD to write Pod config: %v", err)
	}

	absPodConf, err := crictlNewPodConfig(cwd, i.testArgs.Name)
	if err != nil {
		return "", err
	}

	cmdBase := crictlName
	cmdBase += " runp "
	cmdBase += " --runtime=urunc "
	cmdBase += absPodConf

	return commonCmdExec(cmdBase)
}

func (i *crictlInfo) createContainer() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("Failed to get CWD to write Container config: %v", err)
	}

	absContConf, err := crictlNewContainerConfig(cwd, i.testArgs)
	if err != nil {
		return "", err
	}

	// The creation of Pod should have been done before calling
	// createContainer. We do not need to check if the file exists.
	// Let the command fail and return the error.
	absPodConf := filepath.Join(cwd, podConfigFilename)

	cmdBase := crictlName
	cmdBase += " create "
	cmdBase += i.podID + " "
	cmdBase += absContConf + " "
	cmdBase += absPodConf

	return commonCmdExec(cmdBase)
}

func (i *crictlInfo) startContainer(bool) (string, error) {
	cmdBase := crictlName
	cmdBase += " start "
	cmdBase += i.containerID
	return commonCmdExec(cmdBase)
}

func (i *crictlInfo) runContainer(bool) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("Failed to get CWD to write Container/Pod config: %v", err)
	}

	absPodConf, err := crictlNewPodConfig(cwd, i.testArgs.Name)
	if err != nil {
		return "", err
	}

	absContConf, err := crictlNewContainerConfig(cwd, i.testArgs)
	if err != nil {
		return "", err
	}

	cmdBase := crictlName
	cmdBase += " run "
	cmdBase += " --runtime=urunc "
	cmdBase += absContConf + " "
	cmdBase += absPodConf

	return commonCmdExec(cmdBase)
}

func (i *crictlInfo) stopContainer() error {
	output, err := commonStopContainer(crictlName, i.containerID)
	err = checkExpectedOut(i.containerID, output, err)
	if err != nil {
		return fmt.Errorf("Failed to stop %s: %v", i.containerID, err)
	}

	return nil
}

func (i *crictlInfo) stopPod() error {
	cmdBase := crictlName
	cmdBase += " stopp " // spellchecker:disable-line
	cmdBase += i.podID
	output, err := commonCmdExec(cmdBase)
	expectedOutput := fmt.Sprintf("Stopped sandbox %s", i.podID)
	err = checkExpectedOut(expectedOutput, output, err)
	if err != nil {
		return fmt.Errorf("Failed to stop pod %s: %v", i.podID, err)
	}
	return nil
}

func (i *crictlInfo) rmContainer() error {
	output, err := commonRmContainer(crictlName, i.containerID)
	err = checkExpectedOut(i.containerID, output, err)
	if err != nil {
		return fmt.Errorf("Failed to remove %s: %v", i.podID, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("Could not get CWD to remove container Config file: %v", err)
	}
	absContConf := filepath.Join(cwd, cntrConfigFilename)
	err = os.Remove(absContConf)
	if err != nil {
		return fmt.Errorf("Could not remove container config file: %v", err)
	}
	return nil
}

func (i *crictlInfo) rmPod() error {
	cmdBase := crictlName
	cmdBase += " rmp "
	cmdBase += i.podID
	output, err := commonCmdExec(cmdBase)
	expectedOutput := fmt.Sprintf("Removed sandbox %s", i.podID)
	err = checkExpectedOut(expectedOutput, output, err)
	if err != nil {
		return fmt.Errorf("Failed to remove pod %s: %v", i.podID, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("Could not get CWD to remove pod Config file: %v", err)
	}
	absPodConf := filepath.Join(cwd, podConfigFilename)
	err = os.Remove(absPodConf)
	if err != nil {
		return fmt.Errorf("Could not remove Pod config file: %v", err)
	}
	return nil
}

func (i *crictlInfo) logContainer() (string, error) {
	return commonLogs(crictlName, i.containerID)
}

func (i *crictlInfo) searchContainer(cID string) (bool, error) {
	return commonSearchContainer(crictlName, cID)
}

func (i *crictlInfo) searchPod(pID string) (bool, error) {
	cmdBase := crictlName
	cmdBase += " pods "
	cmdBase += " -q "
	cmdBase += " --no-trunc "
	output, err := commonCmdExec(cmdBase)
	if err != nil {
		return true, err
	}

	return searchCID(output, pID), nil
}

func (i *crictlInfo) inspectCAndGet(key string) (string, error) {
	return commonInspectCAndGet(crictlName, i.containerID, key)
}

func (i *crictlInfo) inspectPAndGet(key string) (string, error) {
	cmdBase := crictlName
	cmdBase += " inspectp "
	cmdBase += i.podID
	output, err := commonCmdExec(cmdBase)
	if err != nil {
		return "", err
	}

	return findValOfKey(output, key)
}
