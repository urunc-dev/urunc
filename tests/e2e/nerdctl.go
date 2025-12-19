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

const nerdctlName = "nerdctl"

type nerdctlInfo struct {
	testArgs    containerTestArgs
	containerID string
}

func newNerdctlTool(args containerTestArgs) *nerdctlInfo {
	return &nerdctlInfo{
		testArgs:    args,
		containerID: "",
	}
}

func (i *nerdctlInfo) Name() string {
	return nerdctlName
}

func (i *nerdctlInfo) getTestArgs() containerTestArgs {
	return i.testArgs
}

func (i *nerdctlInfo) getPodID() string {
	// Not supported by nerdctl
	return ""
}

func (i *nerdctlInfo) getContainerID() string {
	return i.containerID
}

func (i *nerdctlInfo) setPodID(string) {
	// Not supported by nerdctl
}

func (i *nerdctlInfo) setContainerID(cID string) {
	i.containerID = cID
}

func (i *nerdctlInfo) createPod() (string, error) {
	// Not supported by nerdctl
	return "", errToolDoesNotSupport
}

func (i *nerdctlInfo) createContainer() (string, error) {
	return commonCreate(nerdctlName, i.testArgs)
}

// nolint:unused
func (i *nerdctlInfo) startPod() (string, error) {
	// Not supported by nerdctl
	return "", errToolDoesNotSupport
}

func (i *nerdctlInfo) startContainer(detach bool) (string, error) {
	return commonStart(nerdctlName, i.containerID, detach)
}

func (i *nerdctlInfo) runContainer(detach bool) (string, error) {
	return commonRun(nerdctlName, i.testArgs, detach)
}

func (i *nerdctlInfo) stopContainer() error {
	output, err := commonStopContainer(nerdctlName, i.containerID)
	err = checkExpectedOut(i.containerID, output, err)
	if err != nil {
		return fmt.Errorf("Failed to stop %s: %v", i.containerID, err)
	}
	return nil
}

func (i *nerdctlInfo) stopPod() error {
	// Not supported by nerdctl
	return errToolDoesNotSupport
}

func (i *nerdctlInfo) rmContainer() error {
	output, err := commonRmContainer(nerdctlName, i.containerID)
	err = checkExpectedOut(i.containerID, output, err)
	if err != nil {
		return fmt.Errorf("Failed to stop %s: %v", i.containerID, err)
	}
	return nil
}

func (i *nerdctlInfo) rmPod() error {
	// Not supported by nerdctl
	return errToolDoesNotSupport
}

func (i *nerdctlInfo) logContainer() (string, error) {
	return commonLogs(nerdctlName, i.containerID)
}

func (i *nerdctlInfo) searchContainer(cID string) (bool, error) {
	return commonSearchContainer(nerdctlName, cID)
}

func (i *nerdctlInfo) searchPod(string) (bool, error) {
	// Not supported by nerdctl
	return true, errToolDoesNotSupport
}

func (i *nerdctlInfo) inspectCAndGet(key string) (string, error) {
	return commonInspectCAndGet(nerdctlName, i.containerID, key)
}

func (i *nerdctlInfo) inspectPAndGet(string) (string, error) {
	// Not supported by nerdctl
	return "", errToolDoesNotSupport
}
