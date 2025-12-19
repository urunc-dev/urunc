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
	"fmt"
)

const dockerName = "docker"

type dockerInfo struct {
	testArgs    containerTestArgs
	containerID string
}

func newDockerTool(args containerTestArgs) *dockerInfo {
	return &dockerInfo{
		testArgs:    args,
		containerID: "",
	}
}

func (i *dockerInfo) Name() string {
	return dockerName
}

func (i *dockerInfo) getTestArgs() containerTestArgs {
	return i.testArgs
}

func (i *dockerInfo) getPodID() string {
	// Not supported by docker
	return ""
}

func (i *dockerInfo) getContainerID() string {
	return i.containerID
}

func (i *dockerInfo) setPodID(string) {
	// Not supported by docker
}

func (i *dockerInfo) setContainerID(cID string) {
	i.containerID = cID
}

func (i *dockerInfo) createPod() (string, error) {
	// Not supported by docker
	return "", errToolDoesNotSupport
}

func (i *dockerInfo) createContainer() (string, error) {
	return commonCreate(dockerName, i.testArgs)
}

// nolint:unused
func (i *dockerInfo) startPod() (string, error) {
	// Not supported by docker
	return "", errToolDoesNotSupport
}

func (i *dockerInfo) startContainer(detach bool) (string, error) {
	return commonStart(dockerName, i.containerID, detach)
}

func (i *dockerInfo) runContainer(detach bool) (string, error) {
	return commonRun(dockerName, i.testArgs, detach)
}

func (i *dockerInfo) stopContainer() error {
	output, err := commonStopContainer(dockerName, i.containerID)
	err = checkExpectedOut(i.containerID, output, err)
	if err != nil {
		return fmt.Errorf("Failed to stop %s: %v", i.containerID, err)
	}
	return nil
}

func (i *dockerInfo) stopPod() error {
	// Not supported by docker
	return errToolDoesNotSupport
}

func (i *dockerInfo) rmContainer() error {
	output, err := commonRmContainer(dockerName, i.containerID)
	err = checkExpectedOut(i.containerID, output, err)
	if err != nil {
		return fmt.Errorf("Failed to stop %s: %v", i.containerID, err)
	}
	return nil
}

func (i *dockerInfo) rmPod() error {
	// Not supported by docker
	return errToolDoesNotSupport
}

func (i *dockerInfo) logContainer() (string, error) {
	return commonLogs(dockerName, i.containerID)
}

func (i *dockerInfo) searchContainer(cID string) (bool, error) {
	return commonSearchContainer(dockerName, cID)
}

func (i *dockerInfo) searchPod(string) (bool, error) {
	// Not supported by docker
	return true, errToolDoesNotSupport
}

func (i *dockerInfo) inspectCAndGet(key string) (string, error) {
	return commonInspectCAndGet(dockerName, i.containerID, key)
}

func (i *dockerInfo) inspectPAndGet(string) (string, error) {
	// Not supported by docker
	return "", errToolDoesNotSupport
}
