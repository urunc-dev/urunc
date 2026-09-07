// Copyright (c) 2023-2026, Nubificus LTD
// SPDX-License-Identifier: Apache-2.0

package initrd

import (
	"fmt"
	"os"
	"time"

	"github.com/cavaliergopher/cpio"
)

// AddFileToInitrd appends a root-owned regular file to an uncompressed
// cpio-newc initrd. It is deliberately platform-neutral: both the Linux
// runtime and the macOS Vz runner add per-container metadata to a private copy
// of the generic boot initrd.
func AddFileToInitrd(oldInitrd, data, name string) error {
	f, err := os.OpenFile(oldInitrd, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("could not open %s: %w", oldInitrd, err)
	}
	defer f.Close()

	w := cpio.NewWriter(f)
	hdr := &cpio.Header{
		Name:    name,
		Mode:    cpio.FileMode(0o100400),
		Uid:     0,
		Guid:    0,
		ModTime: time.Now(),
		Size:    int64(len(data)),
	}
	if err := w.WriteHeader(hdr); err != nil {
		return fmt.Errorf("could not write header for %s: %w", name, err)
	}
	if _, err := w.Write([]byte(data)); err != nil {
		return fmt.Errorf("could not write contents for %s: %w", name, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("could not close initrd after adding %s: %w", name, err)
	}
	return nil
}
