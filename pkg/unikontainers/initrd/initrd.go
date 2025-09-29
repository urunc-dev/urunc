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

package initrd

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/cavaliergopher/cpio"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func AddInitrdRecord(w *cpio.Writer, content []byte, fileInfo *syscall.Stat_t, name string) error {
	hdr := &cpio.Header{
		Name:    name,
		Mode:    cpio.FileMode(fileInfo.Mode),
		Uid:     int(fileInfo.Uid),
		Guid:    int(fileInfo.Gid),
		ModTime: time.Unix(fileInfo.Mtim.Sec, fileInfo.Mtim.Nsec),
		Size:    fileInfo.Size,
	}
	err := w.WriteHeader(hdr)
	if err != nil {
		return fmt.Errorf("could not write header in initrd: %v", err)
	}
	_, err = w.Write(content)
	if err != nil {
		return fmt.Errorf("could not write contents in initrd: %v", err)
	}

	return nil
}

func AddFileToInitrd(w *cpio.Writer, srcFile string, destFile string) error {
	// Get the info of the original file
	fi, err := os.Stat(srcFile)
	if err != nil {
		return fmt.Errorf("Could not Stat file %s: %w", srcFile, err)
	}
	fileInfo := fi.Sys().(*syscall.Stat_t)
	if fi.Mode().IsRegular() {
		content, err := os.ReadFile(srcFile)
		if err != nil {
			return fmt.Errorf("could not read file %s: %w", srcFile, err)
		}
		err = AddInitrdRecord(w, content, fileInfo, destFile)
		if err != nil {
			return fmt.Errorf("could not add record for %s: %w", srcFile, err)
		}
	}

	return nil
}

func CopyFileMountsToInitrd(oldInitrd string, mounts []specs.Mount) error {
	f, err := os.OpenFile(oldInitrd, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("Could not open %s: %v", oldInitrd, err)
	}
	defer f.Close()

	w := cpio.NewWriter(f)
	for _, m := range mounts {
		if m.Type != "bind" {
			continue
		}
		err = AddFileToInitrd(w, m.Source, m.Destination)
		if err != nil {
			return fmt.Errorf("Could not add file %s to initrd: %v", m.Source, err)
		}
	}
	err = w.Close()
	if err != nil {
		return fmt.Errorf("Could not close initrd %v", err)
	}

	return nil
}
