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
