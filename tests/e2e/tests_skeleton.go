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
	"os"
	"strings"
	"testing"
	"time"
)

type testMethod func(tool testTool) error

type containerVolume struct {
	Source string
	Dest   string
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
	StaticNet      bool
	SideContainers []string
	Skippable      bool
	TestFunc       testMethod
	ExpectOut      string
}

func runTest(tool testTool, t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Could not get CWD: %v", err)
	}
	testDir := t.TempDir()
	err = os.Chdir(testDir)
	if err != nil {
		t.Fatalf("Could not change directory to %s: %v", testDir, err)
	}
	t.Cleanup(func() {
		err = os.Chdir(cwd)
		if err != nil {
			t.Errorf("Could not switch back to %s: %v", cwd, err)
		}

	})
	cntrArgs := tool.getTestArgs()
	for _, vol := range cntrArgs.Volumes {
		_, err := os.Stat(vol.Source)
		if err != nil {
			t.Skipf("Could not find %s", vol.Source)
		}
	}
	if cntrArgs.TestFunc == nil {
		if tool.Name() == "crictl" {
			// TODO: Add support for matchTest in crictl
			t.Fatalf("Crictl does not support matchTest")
		}
		output, err := tool.runContainer(false)
		if err != nil {
			t.Errorf("Failed to run unikernel container: %s -- %v", output, err)
		}
		tool.setContainerID(cntrArgs.Name)
		if !strings.Contains(string(output), cntrArgs.ExpectOut) {
			t.Errorf("Expected: %s, Got: %s", cntrArgs.ExpectOut, output)
		}
		err = testCleanup(tool)
		if err != nil {
			t.Errorf("Cleaning up: %v", err)
		}
		return
	}
	podID, err := tool.createPod()
	if err != nil && err != errToolDoesNotSupport {
		t.Fatalf("Failed to create Pod: %s - %v", podID, err)
	}
	tool.setPodID(podID)
	t.Cleanup(func() {
		err = tool.stopPod()
		if err != nil && err != errToolDoesNotSupport {
			t.Errorf("Failed to stop pod: %s - %v", podID, err)
		}

		err = tool.rmPod()
		if err != nil && err != errToolDoesNotSupport {
			t.Errorf("Failed to remove pod: %s - %v", podID, err)
		}
	})
	cID, err := tool.createContainer()
	if err != nil {
		t.Fatalf("Failed to create container: %s - %v", cID, err)
	}
	tool.setContainerID(cID)
	t.Cleanup(func() {
		err = tool.rmContainer()
		if err != nil {
			t.Errorf("Failed to remove container: %s - %v", cntrArgs.Image, err)
		}
		err = testVerifyRm(tool)
		if err != nil {
			t.Errorf("Failed to verify container removal: %s - %v", cntrArgs.Image, err)
		}

	})
	output, err := tool.startContainer(true)
	if err != nil {
		t.Fatalf("Failed to start unikernel container: %s - %v", output, err)
	}
	t.Cleanup(func() {
		err = tool.stopContainer()
		if err != nil {
			t.Errorf("Failed to stop container: %s - %v", cntrArgs.Image, err)
		}

	})
	// Give some time till the unikernel is up and running.
	// Maybe we need to revisit this in the future.
	// MirageOS in Qemu(nested) needs more time to be responsive
	// in ping requests
	time.Sleep(3 * time.Second)
	err = cntrArgs.TestFunc(tool)
	if err != nil {
		t.Fatalf("Failed test: %v", err)
	}
}

func testVerifyRm(tool testTool) error {
	containerID := tool.getContainerID()
	exists, err := tool.searchContainer(containerID)
	if exists || err != nil {
		return fmt.Errorf("Container %s is not removed: %v", containerID, err)
	}
	err = verifyNoStaleFiles(containerID)
	if err != nil {
		return fmt.Errorf("Failed to remove all stale files: %v", err)
	}

	return nil
}

func testCleanup(tool testTool) error {
	err := tool.stopContainer()
	if err != nil {
		return fmt.Errorf("Failed to stop container: %v", err)
	}

	err = tool.rmContainer()
	if err != nil {
		return fmt.Errorf("Failed to remove container: %v", err)
	}

	return testVerifyRm(tool)
}
