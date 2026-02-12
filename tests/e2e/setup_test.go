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
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	testNerdctl    = "TestNerdctl"
	testCrictl     = "TestCrictl"
	testDocker     = "TestDocker"
	maxPullRetries = 5
	pullRetryDelay = 2 * time.Second
)

func TestMain(m *testing.M) {
	flag.Parse()

	testFunc, subtestName := parseTestPattern()
	cases := filterTestCases(testFunc, subtestName)
	images := getTestImages(cases)

	if err := pullAllImages(testFunc, images); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	code := m.Run()

	removeAllImages(testFunc, images)

	os.Exit(code)
}

func parseTestPattern() (string, string) {
	runFlag := flag.Lookup("test.run")
	if runFlag == nil {
		return "", ""
	}
	pattern := runFlag.Value.String()
	if pattern == "" {
		return "", ""
	}
	parts := strings.SplitN(pattern, "/", 2)
	if len(parts) > 1 {
		return parts[0], parts[1]
	}
	return parts[0], ""
}

func getTestCases(testFunc string) []containerTestArgs {
	switch testFunc {
	case testNerdctl:
		return nerdctlTestCases()
	case testCtr:
		// Images managed by BeforeAll in ctr_test.go
		return []containerTestArgs{}
	case testCrictl:
		return crictlTestCases()
	case testDocker:
		return dockerTestCases()
	default:
		var allCases []containerTestArgs
		allCases = append(allCases, nerdctlTestCases()...)
		allCases = append(allCases, crictlTestCases()...)
		allCases = append(allCases, dockerTestCases()...)
		return allCases
	}
}

func filterTestCases(testFunc, subtestName string) []containerTestArgs {
	cases := getTestCases(testFunc)
	if subtestName == "" {
		return cases
	}
	var filtered []containerTestArgs
	for _, tc := range cases {
		if strings.Contains(tc.Name, subtestName) {
			filtered = append(filtered, tc)
		}
	}
	return filtered
}

func getTestImages(cases []containerTestArgs) []string {
	unique := make(map[string]struct{})
	for _, tc := range cases {
		unique[tc.Image] = struct{}{}
	}

	images := make([]string, 0, len(unique))
	for img := range unique {
		images = append(images, img)
	}
	return images
}

func pullAllImages(testFunc string, images []string) error {
	for _, image := range images {
		log.Printf("Pulling image: %s", image)
		if err := pullImageWithRetry(testFunc, image); err != nil {
			return fmt.Errorf("failed to pull %s: %w", image, err)
		}
	}
	return nil
}

func removeAllImages(testFunc string, images []string) {
	for _, image := range images {
		log.Printf("Removing image: %s", image)
		if err := removeImageForTest(testFunc, image); err != nil {
			log.Printf("Warning: failed to remove %s: %v", image, err)
		}
	}
}

func pullImageWithRetry(testFunc string, image string) error {
	var err error
	for i := 0; i < maxPullRetries; i++ {
		err = pullImageForTest(testFunc, image)
		if err == nil {
			return nil
		}

		fmt.Printf("Attempt %d/%d failed to pull %s: %v. Retrying in %v...\n", i+1, maxPullRetries, image, err, pullRetryDelay)
		time.Sleep(pullRetryDelay)
	}
	return fmt.Errorf("failed to pull %s after %d attempts: %w", image, maxPullRetries, err)
}

func pullImageForTest(testFunc string, image string) error {
	switch testFunc {
	case testCrictl:
		cmd := crictlName + " pull " + image
		output, err := commonCmdExec(cmd)
		if err != nil {
			return fmt.Errorf("%s -- %v", output, err)
		}
		return nil
	case testNerdctl:
		return commonPull(nerdctlName, image)
	case testDocker:
		return commonPull(dockerName, image)
	default:
		return commonPull(ctrName, image)
	}
}

func removeImageForTest(testFunc string, image string) error {
	switch testFunc {
	case testCrictl:
		cmd := crictlName + " rmi " + image
		output, err := commonCmdExec(cmd)
		if err != nil {
			return fmt.Errorf("%s -- %v", output, err)
		}
		return nil
	case testNerdctl:
		return commonRmImage(nerdctlName, image)
	case testDocker:
		return commonRmImage(dockerName, image)
	default:
		return commonRmImage(ctrName, image)
	}
}
