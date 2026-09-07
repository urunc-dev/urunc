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
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type testTool interface {
	Name() string
	getTestArgs() containerTestArgs
	getPodID() string
	getContainerID() string
	setPodID(string)
	setContainerID(string)
	createPod() (string, error)
	createContainer() (string, error)
	startContainer(bool) (string, error)
	runContainer(bool) (string, error)
	stopContainer() error
	stopPod() error
	rmContainer() error
	rmPod() error
	logContainer() (string, error)
	searchContainer(string) (bool, error)
	searchPod(string) (bool, error)
	inspectCAndGet(string) (string, error)
	inspectPAndGet(string) (string, error)
}

type testMethod func(tool testTool) error

type containerVolume struct {
	Source string
	Dest   string
}

type sideContainerNetMode string

const (
	// sideContainerNetModeNetwork joins the same named/user-defined
	// network as the primary container (docker/nerdctl --network <name>).
	sideContainerNetModeNetwork sideContainerNetMode = "network"
	// sideContainerNetModeShared joins the primary container's network
	// namespace directly, similar to containers sharing a namespace
	// inside a Kubernetes pod (docker/nerdctl --network container:<id>).
	sideContainerNetModeShared sideContainerNetMode = "shared"
)

type sideContainer struct {
	Name    string
	Image   string
	Cli     string
	Volumes []containerVolume
	NetMode sideContainerNetMode
}

type containerTestArgs struct {
	Name           string
	Image          string
	Devmapper      bool
	Seccomp        bool
	UID            int
	GID            int
	Groups         []int64
	Memory         string
	Cli            string
	Volumes        []containerVolume
	Network        string
	StaticNet      bool
	SideContainers []sideContainer
	Skippable      bool
	TestFunc       testMethod
	ExpectOut      string
}

const (
	testCtr     = "TestCtr"
	testCrictl  = "TestCrictl"
	testDocker  = "TestDocker"
	testNerdctl = "TestNerdctl"
)

var errToolDoesNotSupport = errors.New("Operation not support")

func commonNewContainerCmd(a containerTestArgs) string {
	cmdBase := "--runtime io.containerd.urunc.v2 "
	if a.Devmapper {
		cmdBase += "--snapshotter devmapper "
	}
	if !a.Seccomp {
		cmdBase += "--security-opt seccomp=unconfined "
	}
	if a.Memory != "" {
		cmdBase += fmt.Sprintf("-m %s ", a.Memory)
	}
	if a.Network != "" {
		cmdBase += fmt.Sprintf("--network %s ", a.Network)
	}
	if a.UID != 0 && a.GID != 0 {
		cmdBase += fmt.Sprintf("-u %d:%d ", a.UID, a.GID)
	}
	for _, groupID := range a.Groups {
		cmdBase += fmt.Sprintf("--group-add %d ", groupID)
	}
	for _, vol := range a.Volumes {
		cmdBase += fmt.Sprintf("--mount type=bind,src=%s,dst=%s ", vol.Source, vol.Dest)
	}
	cmdBase += "--name "
	cmdBase += a.Name + " "
	cmdBase += a.Image + " "
	cmdBase += a.Cli
	return cmdBase
}

func commonCmdExec(command string) (output string, err error) {
	var stderrBuf bytes.Buffer

	params := strings.Fields(command)
	cmd := exec.Command(params[0], params[1:]...) //nolint:gosec
	cmd.Stderr = &stderrBuf
	outBytes, err := cmd.Output()
	output = string(outBytes)
	output = strings.TrimSpace(output)
	if err != nil {
		output += strings.TrimSpace(stderrBuf.String())
		return output, err
	}
	return output, nil
}

func commonCmdExecStderr(command string) (string, string, error) {
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	params := strings.Fields(command)
	cmd := exec.Command(params[0], params[1:]...) //nolint:gosec
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	output := strings.TrimSpace(stdoutBuf.String())
	errorOut := strings.TrimSpace(stderrBuf.String())
	return output, errorOut, err
}

func commonSideContainerCmd(sc sideContainer, netArg string) string {
	cmdBase := ""
	if netArg != "" {
		cmdBase += fmt.Sprintf("--network %s ", netArg)
	}
	for _, vol := range sc.Volumes {
		cmdBase += fmt.Sprintf("--mount type=bind,src=%s,dst=%s ", vol.Source, vol.Dest)
	}
	cmdBase += "--name "
	cmdBase += sc.Name + " "
	cmdBase += sc.Image + " "
	cmdBase += sc.Cli
	return cmdBase
}

func commonRunSideContainer(tool string, sc sideContainer, netArg string) (output string, err error) {
	cmdBase := tool + " run -d "
	cmdBase += commonSideContainerCmd(sc, netArg)
	return commonCmdExec(cmdBase)
}

// commonStartSideContainers starts every side container defined and
// joining each one of them int the primary container's network per its NetMode,
// and returns their container IDs for later cleanup.
func commonStartSideContainers(tool string, a containerTestArgs, primaryID string) ([]string, error) {
	var ids []string
	for _, sc := range a.SideContainers {
		var netArg string
		switch sc.NetMode {
		case sideContainerNetModeNetwork:
			// a.Network may be "": docker/nerdctl both attach a
			// container to the default "bridge" network when
			// --network is omitted.
			netArg = a.Network
		case sideContainerNetModeShared:
			netArg = "container:" + primaryID
		default:
			return ids, fmt.Errorf("side container %s has unknown network mode %q", sc.Name, sc.NetMode)
		}
		cID, err := commonRunSideContainer(tool, sc, netArg)
		if err != nil {
			return ids, fmt.Errorf("failed to start side container %s: %s -- %v", sc.Name, cID, err)
		}
		ids = append(ids, cID)
	}
	return ids, nil
}

func commonStopSideContainers(tool string, ids []string) error {
	for _, cID := range ids {
		if _, err := commonStopContainer(tool, cID); err != nil {
			return fmt.Errorf("failed to stop side container %s: %v", cID, err)
		}
	}
	return nil
}

func commonRmSideContainers(tool string, ids []string) error {
	for _, cID := range ids {
		if _, err := commonRmContainer(tool, cID); err != nil {
			return fmt.Errorf("failed to remove side container %s: %v", cID, err)
		}
	}
	return nil
}

func commonPull(tool string, image string) error {
	pullCmd := tool + " image pull " + image

	output, err := commonCmdExec(pullCmd)
	if err != nil {
		return fmt.Errorf("Pull: %s -- %v", output, err)
	}

	return nil
}

func commonRmImage(tool string, image string) error {
	rmCmd := tool + " image rm " + image

	output, err := commonCmdExec(rmCmd)
	if err != nil {
		return fmt.Errorf("Remove image: %s -- %v", output, err)
	}

	return nil
}

func commonCreate(tool string, cntrArgs containerTestArgs) (output string, err error) {
	cmdBase := tool + " create "
	cmdBase += commonNewContainerCmd(cntrArgs)
	return commonCmdExec(cmdBase)
}

func commonStart(tool string, cID string, detach bool) (output string, err error) {
	cmdBase := tool + " start "
	if detach {
		if tool == "ctr t" {
			cmdBase += "--detach "
		}
	} else {
		if tool != "ctr t" {
			cmdBase += "--attach "
		}
	}
	cmdBase += cID
	return commonCmdExec(cmdBase)
}

func commonRun(tool string, cntrArgs containerTestArgs, detach bool) (output string, err error) {
	cmdBase := tool
	cmdBase += " run "
	if detach {
		cmdBase += "-d "
	}
	cmdBase += commonNewContainerCmd(cntrArgs)
	return commonCmdExec(cmdBase)
}

func commonStopContainer(tool string, containerID string) (string, error) {
	cmdBase := tool
	cmdBase += " stop "
	cmdBase += containerID
	return commonCmdExec(cmdBase)
}

func commonRmContainer(tool string, containerID string) (string, error) {
	cmdBase := tool
	cmdBase += " rm "
	cmdBase += containerID
	return commonCmdExec(cmdBase)
}

func commonLogs(tool string, cID string) (string, error) {
	logCmd := tool + " logs " + cID

	return commonCmdExec(logCmd)
}

func commonSearchContainer(tool string, cID string) (bool, error) {
	cmd := tool
	cmd += " ps "
	cmd += " -a "
	cmd += " --no-trunc "
	cmd += " -q"

	output, err := commonCmdExec(cmd)
	if err != nil {
		return true, err
	}
	return searchCID(output, cID), nil
}

func commonInspectCAndGet(tool string, containerID string, key string) (string, error) {
	cmdBase := tool
	cmdBase += " inspect "
	cmdBase += containerID
	output, err := commonCmdExec(cmdBase)
	if err != nil {
		return "", err
	}

	return findValOfKey(output, key)
}

func searchCID(searchArea string, containerID string) bool {
	found := false
	lines := strings.Split(searchArea, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		cID := strings.TrimSpace(line)
		if cID == containerID {
			found = true
			break
		}
	}
	return found
}

func checkExpectedOut(expected string, output string, e error) error {
	if e != nil {
		return fmt.Errorf("%s - %v", output, e)
	}

	if expected != output {
		return fmt.Errorf("Expecting %s, got %s", expected, output)
	}

	return nil
}

func findValOfKey(searchArea string, key string) (string, error) {
	keystr := "\"" + key + "\":[^,;\\]}]*"
	r, err := regexp.Compile(keystr)
	if err != nil {
		return "", err
	}
	match := r.FindString(searchArea)
	if match == "" {
		return "", fmt.Errorf("key %s not found in search area", key)
	}

	keyValMatch := strings.Split(match, ":")
	if len(keyValMatch) < 2 {
		return "", fmt.Errorf("invalid format for key %s: %s", key, match)
	}

	val := strings.ReplaceAll(keyValMatch[1], "\"", "")
	return strings.TrimSpace(val), nil
}
