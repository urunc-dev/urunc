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
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("Nerdctl", Ordered, ContinueOnFailure, Serial, func() {
	var tool *nerdctlInfo

	BeforeAll(func() {
		DeferCleanup(func() {
			cleanupImages(ToolNerdctl)
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
				skipMissingVolumes(tc)
				tool = newNerdctlTool(tc)
				runForegroundTest(tool, tc)
			},
			toTableEntries(selectTestCases(nerdctlTestCases(), false)),
		)
	})

	Context("detached containers", func() {
		DescribeTable("unikernel containers",
			func(tc containerTestArgs) {
				skipMissingVolumes(tc)
				tool = newNerdctlTool(tc)
				runDetachedTest(tool, tc)
			},
			toTableEntries(selectTestCases(nerdctlTestCases(), true)),
		)
	})
})
