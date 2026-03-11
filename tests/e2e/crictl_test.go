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

var _ = Describe("Crictl", Ordered, ContinueOnFailure, func() {
	var tool *crictlInfo

	BeforeAll(func() {
		cases := crictlTestCases()
		images := getTestImages(cases)
		err := pullAllImages(testCrictl, images)
		Expect(err).NotTo(HaveOccurred(), "Failed to pull crictl images")

		DeferCleanup(func() {
			removeAllImages(testCrictl, images)
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

	DescribeTable("unikernel containers",
		func(tc containerTestArgs) {
			for _, vol := range tc.Volumes {
				if _, err := os.Stat(vol.Source); err != nil {
					Skip(fmt.Sprintf("Could not find %s", vol.Source))
				}
			}

			tool = newCrictlTool(tc)

			By("Creating pod")
			pID, err := tool.createPod()
			Expect(err).NotTo(HaveOccurred(), "Failed to create pod: %s", pID)
			tool.setPodID(pID)

			DeferCleanup(func() {
				if tool != nil && tool.getPodID() != "" {
					By("Stopping pod")
					if err := tool.stopPod(); err != nil {
						GinkgoLogr.Error(err, "Failed to stop pod")
					}
					By("Removing pod")
					if err := tool.rmPod(); err != nil {
						GinkgoLogr.Error(err, "Failed to remove pod")
					}
				}
			})

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
		toTableEntries(crictlTestCases()),
	)
})
