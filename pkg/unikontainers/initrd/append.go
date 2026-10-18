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

package initrd

import (
	"fmt"
	"os"
	"time"

	"github.com/cavaliergopher/cpio"
)

// appendedFileMode is S_IFREG|0400, a regular file readable by its owner.
const appendedFileMode cpio.FileMode = 0o100400

// AddFileToInitrd appends a newc archive with one file to oldInitrd.
//
// The file is called name and holds data. It is owned by root, has inode 0
// and carries the current time as its mtime. The kernel unpacks the
// appended archive over the earlier ones, so the file replaces an earlier
// file of the same name.
func AddFileToInitrd(oldInitrd string, data string, name string) error {
	f, err := os.OpenFile(oldInitrd, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("could not open %s: %w", oldInitrd, err)
	}
	defer f.Close()

	w := cpio.NewWriter(f)
	hdr := &cpio.Header{
		Name:    name,
		Inode:   0,
		Mode:    appendedFileMode,
		Uid:     0,
		Guid:    0,
		ModTime: time.Unix(time.Now().Unix(), 0),
		Size:    int64(len(data)),
	}
	if err := w.WriteHeader(hdr); err != nil {
		return fmt.Errorf("could not add file %s to initrd: could not write header in initrd: %w", name, err)
	}
	if _, err := w.Write([]byte(data)); err != nil {
		return fmt.Errorf("could not add file %s to initrd: could not write contents in initrd: %w", name, err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("could not close initrd: %w", err)
	}

	return nil
}
