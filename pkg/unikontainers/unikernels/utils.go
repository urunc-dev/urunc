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
	"fmt"
	"net"
	"os"
)

func subnetMaskToCIDR(subnetMask string) (int, error) {
	ip := net.ParseIP(subnetMask).To4()
	if ip == nil {
		return 0, fmt.Errorf("invalid subnet mask format: %s", subnetMask)
	}

	mask := net.IPMask(ip)
	ones, bits := mask.Size()
	if bits != 32 {
		return 0, fmt.Errorf("invalid (non-contiguous) subnet mask: %s", subnetMask)
	}

	return ones, nil
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
