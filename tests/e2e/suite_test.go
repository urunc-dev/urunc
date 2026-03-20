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
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

const (
	defaultTimeout  = 10 * time.Second
	defaultInterval = 1 * time.Second
)

func TestE2E(t *testing.T) {
	format.MaxLength = 0 // Do not truncate failure output
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

// setupTestDir switches to a temp directory, restored via DeferCleanup.
func setupTestDir() {
	cwd, err := os.Getwd()
	Expect(err).NotTo(HaveOccurred())
	testDir := GinkgoT().TempDir()
	Expect(os.Chdir(testDir)).To(Succeed())
	DeferCleanup(func() {
		Expect(os.Chdir(cwd)).To(Succeed())
	})
}

// toTableEntries converts a slice of containerTestArgs into Ginkgo TableEntry
// instances for use with DescribeTable.
func toTableEntries(cases []containerTestArgs) []TableEntry {
	entries := make([]TableEntry, 0, len(cases))
	for _, tc := range cases {
		entries = append(entries, Entry(tc.Name, tc))
	}
	return entries
}

// selectTestCases returns cases with or without a TestFunc.
func selectTestCases(cases []containerTestArgs, hasTestFunc bool) []containerTestArgs {
	var out []containerTestArgs
	for _, tc := range cases {
		if (tc.TestFunc != nil) == hasTestFunc {
			out = append(out, tc)
		}
	}
	return out
}

// skipMissingVolumes skips the test if any required volume source is missing.
func skipMissingVolumes(tc containerTestArgs) {
	for _, vol := range tc.Volumes {
		if _, err := os.Stat(vol.Source); err != nil {
			Skip(fmt.Sprintf("Could not find %s", vol.Source))
		}
	}
}

// runDetachedTest runs a container in detached mode: create, start, and
// verify via TestFunc.
func runDetachedTest(tool testTool, tc containerTestArgs) {
	By("Creating container")
	cID, err := tool.createContainer()
	Expect(err).NotTo(HaveOccurred(), "Failed to create container: %s", cID)
	tool.setContainerID(cID)

	DeferCleanup(func() {
		if tool.getContainerID() != "" {
			By("Stopping container")
			if err := tool.stopContainer(); err != nil {
				GinkgoLogr.Error(err, "Failed to stop container")
			}
			By("Removing container")
			if err := tool.rmContainer(); err != nil {
				GinkgoLogr.Error(err, "Failed to remove container")
			}
			By("Verifying container removal")
			if err := testVerifyRm(tool); err != nil {
				GinkgoLogr.Error(err, "Failed to verify removal")
			}
		}
	})

	By("Starting container")
	output, err := tool.startContainer(true)
	Expect(err).NotTo(HaveOccurred(), "Failed to start container: %s", output)

	By("Running test function")
	Eventually(func() error {
		return tc.TestFunc(tool)
	}, defaultTimeout, defaultInterval).Should(Succeed())
}

// runForegroundTest runs a container in the foreground and verifies the
// output contains the expected string.
func runForegroundTest(tool testTool, tc containerTestArgs) {
	tool.setContainerID(tc.Name)

	DeferCleanup(func() {
		if tool.getContainerID() != "" {
			By("Cleaning up container")
			if err := testCleanup(tool); err != nil {
				GinkgoLogr.Error(err, "Container cleanup failed")
			}
		}
	})

	By("Running container")
	output, err := tool.runContainer(false)
	Expect(err).NotTo(HaveOccurred(), "Failed to run container: %s", output)

	By("Verifying container output")
	Expect(output).To(ContainSubstring(tc.ExpectOut))
}
