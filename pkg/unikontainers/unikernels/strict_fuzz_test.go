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

package unikernels

import (
	"os"
	"strings"
)

// strictOn reports whether the strict check name is on in URUNC_FUZZ_STRICT: "1" or "all" for
// every one, or a list of names. A target that fails on main today returns early unless it is on.
func strictOn(name string) bool {
	for _, on := range strings.Split(os.Getenv("URUNC_FUZZ_STRICT"), ",") {
		if on = strings.TrimSpace(on); on == "1" || on == "all" || on == name {
			return true
		}
	}
	return false
}
