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
	"log"
	"sort"
	"sync"
)

// imageCache pulls an image the first time a test asks for it and remembers
// what it pulled.
//
// The suites used to pull every image of a tool up front in BeforeAll. That
// ignores --ginkgo.focus: `make test_nerdctl_Spt` runs the Spt subset but
// still paid for every image in nerdctlTestCases(), and CI runs the filtered
// targets on every job (.github/workflows/vm_test.yml). Pulling from the spec
// that needs the image means a filtered run fetches only what it runs,
// without the cache having to know anything about focus strings.
//
// pulled also drives cleanup, so a filtered run removes the images it brought
// in rather than every image the tool declares.
type imageCache struct {
	mu     sync.Mutex
	pull   func(testFunc string, image string) error
	pulled map[string]struct{}
}

func newImageCache() *imageCache {
	return &imageCache{
		pull:   pullImageWithRetry,
		pulled: make(map[string]struct{}),
	}
}

// ensure pulls image unless this cache already pulled it successfully.
// A failed pull is not recorded, so a later spec wanting the same image
// retries rather than assuming it is present.
func (c *imageCache) ensure(testFunc string, image string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.pulled[image]; ok {
		return nil
	}

	log.Printf("Pulling image: %s", image)
	if err := c.pull(testFunc, image); err != nil {
		return err
	}

	c.pulled[image] = struct{}{}
	return nil
}

// images returns the images this cache pulled, sorted so cleanup order is
// stable between runs.
func (c *imageCache) images() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	images := make([]string, 0, len(c.pulled))
	for image := range c.pulled {
		images = append(images, image)
	}
	sort.Strings(images)
	return images
}
