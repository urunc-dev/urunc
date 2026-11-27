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
	"errors"
	"reflect"
	"sync"
	"testing"
)

// newTestCache returns a cache whose pull is recorded instead of performed.
func newTestCache(err error) (*imageCache, *[]string) {
	var calls []string
	c := &imageCache{
		pulled: make(map[string]struct{}),
		pull: func(_ string, image string) error {
			calls = append(calls, image)
			return err
		},
	}
	return c, &calls
}

func TestImageCachePullsEachImageOnce(t *testing.T) {
	c, calls := newTestCache(nil)

	for range 3 {
		if err := c.ensure("ctr", "alpine"); err != nil {
			t.Fatalf("ensure: %v", err)
		}
	}
	if err := c.ensure("ctr", "nginx"); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	want := []string{"alpine", "nginx"}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("pulls = %v, want %v", *calls, want)
	}
}

func TestImageCacheReportsOnlyWhatItPulled(t *testing.T) {
	c, _ := newTestCache(nil)

	// Out of order, to check images() sorts rather than returning map order.
	for _, image := range []string{"nginx", "alpine", "nginx"} {
		if err := c.ensure("ctr", image); err != nil {
			t.Fatalf("ensure: %v", err)
		}
	}

	want := []string{"alpine", "nginx"}
	if got := c.images(); !reflect.DeepEqual(got, want) {
		t.Errorf("images() = %v, want %v", got, want)
	}
}

// A filtered run must not remove images it never pulled: cleanup is driven by
// images(), so an empty run has nothing to tear down.
func TestImageCacheIsEmptyBeforeAnyPull(t *testing.T) {
	c, _ := newTestCache(nil)

	if got := c.images(); len(got) != 0 {
		t.Errorf("images() = %v, want empty", got)
	}
}

func TestImageCacheDoesNotRecordFailedPulls(t *testing.T) {
	boom := errors.New("pull failed")
	c, calls := newTestCache(boom)

	if err := c.ensure("ctr", "alpine"); !errors.Is(err, boom) {
		t.Fatalf("ensure error = %v, want %v", err, boom)
	}
	if got := c.images(); len(got) != 0 {
		t.Errorf("images() = %v after a failed pull, want empty", got)
	}

	// A later spec wanting the same image must retry rather than assume it
	// is present.
	if err := c.ensure("ctr", "alpine"); !errors.Is(err, boom) {
		t.Fatalf("second ensure error = %v, want %v", err, boom)
	}
	if want := []string{"alpine", "alpine"}; !reflect.DeepEqual(*calls, want) {
		t.Errorf("pulls = %v, want %v", *calls, want)
	}
}

func TestImageCacheEnsureIsConcurrencySafe(t *testing.T) {
	c, calls := newTestCache(nil)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.ensure("ctr", "alpine"); err != nil {
				t.Errorf("ensure: %v", err)
			}
		}()
	}
	wg.Wait()

	if len(*calls) != 1 {
		t.Errorf("pulled %d times, want 1", len(*calls))
	}
}
