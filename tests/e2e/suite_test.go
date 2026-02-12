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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

func TestCtr(t *testing.T) {
	format.MaxLength = 0 // Do not truncate failure output
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ctr E2E Suite")
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
