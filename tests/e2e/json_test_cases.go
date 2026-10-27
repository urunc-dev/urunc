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
	"encoding/json"
	"fmt"
	"os"
)

// jsonTestCase is the JSON-serializable form of containerTestArgs.
// TestFunc is omitted — JSON cases always have a nil TestFunc, which
// routes them to runForegroundTest (output matching via ExpectOut).
type jsonTestCase struct {
	Image          string            `json:"image"`
	Name           string            `json:"name"`
	Devmapper      bool              `json:"devmapper"`
	Seccomp        bool              `json:"seccomp"`
	UID            int               `json:"uid"`
	GID            int               `json:"gid"`
	Groups         []int64           `json:"groups"`
	Memory         string            `json:"memory"`
	Cli            string            `json:"cli"`
	Volumes        []containerVolume `json:"volumes"`
	StaticNet      bool              `json:"staticNet"`
	SideContainers []string          `json:"sideContainers"`
	Skippable      bool              `json:"skippable"`
	ExpectOut      string            `json:"expectOut"`
}

func (j jsonTestCase) toContainerTestArgs() containerTestArgs {
	groups := j.Groups
	if groups == nil {
		groups = []int64{}
	}
	volumes := j.Volumes
	if volumes == nil {
		volumes = []containerVolume{}
	}
	sideContainers := j.SideContainers
	if sideContainers == nil {
		sideContainers = []string{}
	}
	return containerTestArgs{
		Image:          j.Image,
		Name:           j.Name,
		Devmapper:      j.Devmapper,
		Seccomp:        j.Seccomp,
		UID:            j.UID,
		GID:            j.GID,
		Groups:         groups,
		Memory:         j.Memory,
		Cli:            j.Cli,
		Volumes:        volumes,
		StaticNet:      j.StaticNet,
		SideContainers: sideContainers,
		Skippable:      j.Skippable,
		ExpectOut:      j.ExpectOut,
	}
}

// loadTestCasesFromJSON reads a JSON array of test cases from path.
func loadTestCasesFromJSON(path string) ([]containerTestArgs, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cases []jsonTestCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", path, err)
	}

	result := make([]containerTestArgs, len(cases))
	for i, c := range cases {
		result[i] = c.toContainerTestArgs()
	}
	return result, nil
}

// optionalJSONTestCases returns test cases from path, or an empty slice if
// the file does not exist. Parse errors are reported to stderr.
func optionalJSONTestCases(path string) []containerTestArgs {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return []containerTestArgs{}
	}
	cases, err := loadTestCasesFromJSON(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load JSON test cases from %s: %v\n", path, err) //nolint:errcheck
		return []containerTestArgs{}
	}
	return cases
}
