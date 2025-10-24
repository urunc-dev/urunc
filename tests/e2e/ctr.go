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

const ctrName = "ctr"

type ctrInfo struct {
	testArgs    containerTestArgs
	containerID string
	detached    bool
}

func newCtrTool(args containerTestArgs) *ctrInfo {
	return &ctrInfo{
		testArgs:    args,
		containerID: "",
		detached:    false,
	}
}

func ctrNewContainerCmd(a containerTestArgs) string {
	cmdBase := "--runtime io.containerd.urunc.v2 "
	if a.Devmapper {
		cmdBase += "--snapshotter devmapper "
	}
	if a.Seccomp {
		cmdBase += "--seccomp "
	}
	if a.Memory != "" {
		cmdBase += fmt.Sprintf("--memory-limit %s ", a.Memory)
	}
	// ctr does not seem to support additional groups.
	// Therefore only set the UID/GID
	if a.UID != 0 || a.GID != 0 {
		cmdBase += fmt.Sprintf("-u %d:%d ", a.UID, a.GID)
	}
	cmdBase += a.Image + " "
	cmdBase += a.Name + " "
	cmdBase += a.Cli
	return cmdBase
}

func (i *ctrInfo) Name() string {
	return ctrName
}

func (i *ctrInfo) getTestArgs() containerTestArgs {
	return i.testArgs
}

func (i *ctrInfo) getPodID() string {
	// Not supported by ctr
	return ""
}

func (i *ctrInfo) getContainerID() string {
	return i.containerID
}

func (i *ctrInfo) setPodID(string) {
	// Not supported by ctr
}

func (i *ctrInfo) setContainerID(cID string) {
	i.containerID = cID
}

func (i *ctrInfo) pullImage() error {
	return commonPull(ctrName, i.testArgs.Image)
}

func (i *ctrInfo) rmImage() error {
	return commonRmImage(ctrName, i.testArgs.Image)
}

func (i *ctrInfo) createPod() (string, error) {
	// Not supported by ctr
	return "", errToolDoesNotSupport
}

func (i *ctrInfo) createContainer() (string, error) {
	cmdBase := ctrName
	cmdBase += " c create "
	cmdBase += ctrNewContainerCmd(i.testArgs)
	return commonCmdExec(cmdBase)
}

// nolint:unused
func (i *ctrInfo) startPod() (string, error) {
	// Not supported by ctr
	return "", errToolDoesNotSupport
}

func (i *ctrInfo) startContainer(detach bool) (string, error) {
	if detach {
		i.detached = true
	}
	return commonStart(ctrName+" t", i.containerID, detach)
}

func (i *ctrInfo) runContainer(detach bool) (string, error) {
	cmdBase := ctrName
	cmdBase += " run "
	if detach {
		cmdBase += "-d "
		i.detached = true
	}
	cmdBase += ctrNewContainerCmd(i.testArgs)
	return commonCmdExec(cmdBase)
}

func (i *ctrInfo) stopContainer() error {
	if !i.detached {
		return nil
	}
	cmdBase := ctrName
	cmdBase += " t kill "
	cmdBase += i.containerID
	output, err := commonCmdExec(cmdBase)
	err = checkExpectedOut("", output, err)
	if err != nil {
		return fmt.Errorf("Failed to stop %s: %v", i.containerID, err)
	}
	return nil
}

func (i *ctrInfo) stopPod() error {
	// Not supported by ctr
	return errToolDoesNotSupport
}

func (i *ctrInfo) rmContainer() error {
	output, err := commonRmContainer(ctrName+" c", i.containerID)
	err = checkExpectedOut("", output, err)
	if err != nil {
		return fmt.Errorf("Failed to remove %s: %v", i.containerID, err)
	}
	output, err = commonRmContainer(ctrName+" s", i.testArgs.Name)
	err = checkExpectedOut("", output, err)
	if err != nil {
		return fmt.Errorf("Failed to remove snapshot %s: %v", i.testArgs.Name, err)
	}
	return nil
}

func (i *ctrInfo) rmPod() error {
	// Not supported by ctr
	return errToolDoesNotSupport
}

func (i *ctrInfo) logContainer() (string, error) {
	// Not supported by ctr
	// TODO: We need to fix this
	// One idea would be to use a go routine for the running container
	// and channels for redirecting the output and getting the logs
	return "", errToolDoesNotSupport
}

func (i *ctrInfo) searchContainer(cID string) (bool, error) {
	cmd := ctrName + " c ls -q"

	output, err := commonCmdExec(cmd)
	if err != nil {
		return true, err
	}
	return searchCID(output, cID), nil
}

func (i *ctrInfo) searchPod(string) (bool, error) {
	// Not supported by ctr
	return true, errToolDoesNotSupport
}

func (i *ctrInfo) inspectCAndGet(key string) (string, error) {
	cmdBase := ctrName
	cmdBase += " c info "
	cmdBase += i.containerID
	output, err := commonCmdExec(cmdBase)
	if err != nil {
		return "", err
	}

	return findValOfKey(output, key)
}

func (i *ctrInfo) inspectPAndGet(string) (string, error) {
	// Not supported by ctr
	return "", errToolDoesNotSupport
}
