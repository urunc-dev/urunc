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
)

func TestNerdctl(t *testing.T) {
	kvmGroup, err := getKVMGroupID()
	if err != nil {
		t.Errorf("Failed to get KVM grou id")
	}
	tests := nerdctlTestCases(kvmGroup)
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			nerdctlTool := newNerdctlTool(tc)
			runTest(nerdctlTool, t)
		})
	}
}

func TestCrictl(t *testing.T) {
	kvmGroup, err := getKVMGroupID()
	if err != nil {
		t.Errorf("Failed to get KVM grou id")
	}
	tests := crictlTestCases(kvmGroup)
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			crictlTool := newCrictlTool(tc)
			runTest(crictlTool, t)
		})
	}
}

func TestDocker(t *testing.T) {
	kvmGroup, err := getKVMGroupID()
	if err != nil {
		t.Errorf("Failed to get KVM grou id")
	}
	tests := dockerTestCases(kvmGroup)
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			dockerTool := newDockerTool(tc)
			runTest(dockerTool, t)
		})
	}
}
