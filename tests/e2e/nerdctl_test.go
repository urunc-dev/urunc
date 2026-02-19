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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testNerdctl = "TestNerdctl"

var _ = Describe("Nerdctl", Ordered, ContinueOnFailure, func() {
	var tool *nerdctlInfo

	BeforeAll(func() {
		cases := nerdctlTestCases()
		images := getTestImages(cases)
		err := pullAllImages(testNerdctl, images)
		Expect(err).NotTo(HaveOccurred(), "Failed to pull nerdctl images")

		DeferCleanup(func() {
			removeAllImages(testNerdctl, images)
		})
	})

	BeforeEach(func() {
		setupTestDir()
	})

	ReportAfterEach(func(report SpecReport) {
		if report.Failed() && tool != nil {
			AddReportEntry("test-args", tool.getTestArgs())
		}
	})

	Context("foreground containers", func() {
		DescribeTable("unikernel containers",
			func(tc containerTestArgs) {
				for _, vol := range tc.Volumes {
					if _, err := os.Stat(vol.Source); err != nil {
						Skip(fmt.Sprintf("Could not find %s", vol.Source))
					}
				}

				tool = newNerdctlTool(tc)
				tool.setContainerID(tc.Name)

				DeferCleanup(func() {
					if tool != nil && tool.getContainerID() != "" {
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
			},
			toTableEntries(selectTestCases(nerdctlTestCases(), false)),
		)
	})

	Context("detached containers", func() {
		DescribeTable("unikernel containers",
			func(tc containerTestArgs) {
				for _, vol := range tc.Volumes {
					if _, err := os.Stat(vol.Source); err != nil {
						Skip(fmt.Sprintf("Could not find %s", vol.Source))
					}
				}

				tool = newNerdctlTool(tc)

				By("Creating container")
				cID, err := tool.createContainer()
				Expect(err).NotTo(HaveOccurred(), "Failed to create container: %s", cID)
				tool.setContainerID(cID)

				DeferCleanup(func() {
					if tool != nil && tool.getContainerID() != "" {
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
			},
			toTableEntries(selectTestCases(nerdctlTestCases(), true)),
		)
	})
})
