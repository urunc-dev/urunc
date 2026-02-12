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

const testCtr = "TestCtr"

var _ = Describe("Ctr", Ordered, ContinueOnFailure, func() {
	var (
		tool *ctrInfo
		cwd  string
	)

	BeforeAll(func() {
		cases := ctrTestCases()
		images := getTestImages(cases)
		err := pullAllImages(testCtr, images)
		Expect(err).NotTo(HaveOccurred(), "Failed to pull ctr images")

		DeferCleanup(func() {
			removeAllImages(testCtr, images)
		})
	})

	BeforeEach(func() {
		var err error
		cwd, err = os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		testDir := GinkgoT().TempDir()
		Expect(os.Chdir(testDir)).To(Succeed())

		DeferCleanup(func() {
			Expect(os.Chdir(cwd)).To(Succeed())
		})
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

			tool = newCtrTool(tc)
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
			Expect(err).NotTo(HaveOccurred(), "Failed to run unikernel container: %s", output)

			By("Verifying container output")
			Expect(output).To(ContainSubstring(tc.ExpectOut))
		},
		toTableEntries(ctrTestCases()),
	)
})
