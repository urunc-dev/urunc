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

// Package initrd provides utilities for creating and manipulating CPIO initrd archives.
package initrd

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/cavaliergopher/cpio"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// AddInitrdRecord adds a record to the initrd CPIO archive with the given content and metadata.
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

// CopyFileToInitrd copies a file from the host to the initrd CPIO archive.
func CopyFileToInitrd(w *cpio.Writer, srcFile string, destFile string) error {
	// Get the info of the original file
	fi, err := os.Stat(srcFile)
	if err != nil {
		return fmt.Errorf("could not stat file %s: %w", srcFile, err)
	}
	fileInfo := fi.Sys().(*syscall.Stat_t)
	if fi.Mode().IsRegular() {
		content, err := os.ReadFile(srcFile) //nolint:gosec
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

// CopyFileMountsToInitrd copies mounted files to an existing initrd archive.
func CopyFileMountsToInitrd(oldInitrd string, mounts []specs.Mount) error {
	f, err := os.OpenFile(oldInitrd, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec
	if err != nil {
		return fmt.Errorf("could not open %s: %v", oldInitrd, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Printf("failed to close initrd file %s: %v\n", oldInitrd, err)
		}
	}()

	w := cpio.NewWriter(f)
	for _, m := range mounts {
		if m.Type != "bind" {
			continue
		}
		err = CopyFileToInitrd(w, m.Source, m.Destination)
		if err != nil {
			return fmt.Errorf("could not add file %s to initrd: %v", m.Source, err)
		}
	}
	err = w.Close()
	if err != nil {
		return fmt.Errorf("could not close initrd %v", err)
	}

	return nil
}

// AddFileToInitrd adds a file with the given data to an existing initrd archive.
func AddFileToInitrd(oldInitrd string, data string, name string) error {
	f, err := os.OpenFile(oldInitrd, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec
	if err != nil {
		return fmt.Errorf("could not open %s: %v", oldInitrd, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Printf("failed to close initrd file %s: %v\n", oldInitrd, err)
		}
	}()

	w := cpio.NewWriter(f)
	fileInfo := syscall.Stat_t{
		Mode: syscall.S_IFREG | 0400,
		Size: int64(len(data)),
		Mtim: syscall.Timespec{Sec: time.Now().Unix()},
		Uid:  0,
		Gid:  0,
	}
	err = AddInitrdRecord(w, []byte(data), &fileInfo, name)
	if err != nil {
		return fmt.Errorf("could not add file %s to initrd: %v", name, err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("could not close initrd %v", err)
	}

	return nil
}
