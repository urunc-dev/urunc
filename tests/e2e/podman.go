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

package urunce2etesting

import (
	"fmt"
	"os"
	"strings"
)

const podmanName = "podman"

type podmanInfo struct {
	testArgs    containerTestArgs
	containerID string
}

func newPodmanTool(args containerTestArgs) *podmanInfo {
	return &podmanInfo{
		testArgs:    args,
		containerID: "",
	}
}

func (i *podmanInfo) Name() string {
	return podmanName
}

func (i *podmanInfo) getTestArgs() containerTestArgs {
	return i.testArgs
}

func (i *podmanInfo) getPodID() string {
	// Not supported by podman
	return ""
}

func (i *podmanInfo) getContainerID() string {
	return i.containerID
}

func (i *podmanInfo) setPodID(string) {
	// Not supported by podman
}

func (i *podmanInfo) setContainerID(cID string) {
	i.containerID = cID
}

func (i *podmanInfo) createPod() (string, error) {
	// Not supported by podman
	return "", errToolDoesNotSupport
}

func (i *podmanInfo) createContainer() (string, error) {
	return commonCreate(podmanName, i.testArgs)
}

// nolint:unused
func (i *podmanInfo) startPod() (string, error) {
	// Not supported by podman
	return "", errToolDoesNotSupport
}

func (i *podmanInfo) startContainer(detach bool) (string, error) {
	return commonStart(podmanName, i.containerID, detach)
}

func (i *podmanInfo) runContainer(detach bool) (string, error) {
	return commonRun(podmanName, i.testArgs, detach)
}

func (i *podmanInfo) stopContainer() error {
	output, err := commonStopContainer(podmanName, i.containerID)
	err = checkExpectedOut(i.containerID, output, err)
	if err != nil {
		return fmt.Errorf("Failed to stop %s: %v", i.containerID, err)
	}
	return nil
}

func (i *podmanInfo) stopPod() error {
	// Not supported by podman
	return errToolDoesNotSupport
}

func (i *podmanInfo) rmContainer() error {
	output, err := commonRmContainer(podmanName, i.containerID)
	err = checkExpectedOut(i.containerID, output, err)
	if err != nil {
		return fmt.Errorf("Failed to stop %s: %v", i.containerID, err)
	}
	return nil
}

func (i *podmanInfo) rmPod() error {
	// Not supported by podman
	return errToolDoesNotSupport
}

func (i *podmanInfo) logContainer() (string, error) {
	return commonLogs(podmanName, i.containerID)
}

func (i *podmanInfo) searchContainer(cID string) (bool, error) {
	return commonSearchContainer(podmanName, cID)
}

func (i *podmanInfo) searchPod(string) (bool, error) {
	// Not supported by podman
	return true, errToolDoesNotSupport
}

func (i *podmanInfo) inspectCAndGet(key string) (string, error) {
	return commonInspectCAndGet(podmanName, i.containerID, key)
}

func (i *podmanInfo) inspectPAndGet(string) (string, error) {
	// Not supported by podman
	return "", errToolDoesNotSupport
}

func (i *podmanInfo) configPath() (string, error) {
	cmd := podmanName + " inspect --type container --format={{.OCIConfigPath}} " + i.containerID
	out, err := commonCmdExec(cmd)
	out = strings.TrimSpace(out)
	if err != nil {
		if out != "" {
			return "", fmt.Errorf("%v (output: %s)", err, out)
		}
		return "", err
	}
	if out == "" {
		return "", fmt.Errorf("empty config path")
	}
	if _, statErr := os.Stat(out); statErr != nil {
		return "", fmt.Errorf("config path not found: %s (%v)", out, statErr)
	}
	return out, nil
}
