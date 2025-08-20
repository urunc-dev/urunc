// Copyright (c) 2023-2025, Nubificus LTD
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
	"fmt"
	"os"
	"strconv"
	"strings"
)

func subnetMaskToCIDR(subnetMask string) (int, error) {
	maskParts := strings.Split(subnetMask, ".")
	if len(maskParts) != 4 {
		return 0, fmt.Errorf("invalid subnet mask format")
	}

	var cidr int
	for _, part := range maskParts {
		val, err := strconv.Atoi(part)
		if err != nil || val < 0 || val > 255 {
			return 0, fmt.Errorf("invalid subnet mask value: %s", part)
		}

		// Convert part to binary and count the number of 1 bits
		binary := fmt.Sprintf("%08b", val)
		cidr += strings.Count(binary, "1")
	}

	return cidr, nil
}

func createFile(path string, content string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	if err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	return nil
}
