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
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cavaliergopher/cpio"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const maxNewcInode int64 = 1<<32 - 1

const (
	newcHeaderSize = 110
	trailerName    = "TRAILER!!!"
)

// inodeAllocator prevents newly merged entries from colliding with existing
// c_ino values. Unikraft treats repeated inodes with multiple links as hard
// links, even when the records have different file types.
type inodeAllocator struct {
	used map[int64]struct{}
	next int64
}

func newInodeAllocator() *inodeAllocator {
	return &inodeAllocator{
		used: make(map[int64]struct{}),
		next: 1,
	}
}

func (a *inodeAllocator) reserve(inode int64) {
	a.used[inode] = struct{}{}
}

func (a *inodeAllocator) allocate() (int64, error) {
	for a.next <= maxNewcInode {
		inode := a.next
		a.next++
		if _, exists := a.used[inode]; exists {
			continue
		}
		a.used[inode] = struct{}{}
		return inode, nil
	}

	return 0, errors.New("newc inode space exhausted")
}

func addInitrdRecord(w *cpio.Writer, content []byte, fileInfo *syscall.Stat_t, name string, inode int64) error {
	hdr := &cpio.Header{
		Name:    name,
		Inode:   inode,
		Mode:    cpio.FileMode(fileInfo.Mode),
		Uid:     int(fileInfo.Uid),
		Guid:    int(fileInfo.Gid),
		ModTime: time.Unix(fileInfo.Mtim.Sec, fileInfo.Mtim.Nsec),
		Size:    fileInfo.Size,
	}
	err := w.WriteHeader(hdr)
	if err != nil {
		return fmt.Errorf("could not write header in initrd: %w", err)
	}
	_, err = w.Write(content)
	if err != nil {
		return fmt.Errorf("could not write contents in initrd: %w", err)
	}

	return nil
}

func CopyFileToInitrd(w *cpio.Writer, srcFile string, destFile string) error {
	return copyFileToInitrdWithInode(w, srcFile, destFile, 0)
}

func copyFileToInitrdWithInode(w *cpio.Writer, srcFile string, destFile string, inode int64) error {
	// Get the info of the original file
	fi, err := os.Stat(srcFile)
	if err != nil {
		return fmt.Errorf("could not stat file %s: %w", srcFile, err)
	}
	fileInfo := fi.Sys().(*syscall.Stat_t)
	if fi.Mode().IsRegular() {
		content, err := os.ReadFile(srcFile)
		if err != nil {
			return fmt.Errorf("could not read file %s: %w", srcFile, err)
		}
		err = addInitrdRecord(w, content, fileInfo, destFile, inode)
		if err != nil {
			return fmt.Errorf("could not add record for %s: %w", srcFile, err)
		}
	}

	return nil
}

// CopyFileMountsToInitrd appends the files of the given mounts to the initrd
// as a new archive member. The caller should pass only bind mounts.
func CopyFileMountsToInitrd(oldInitrd string, mounts []specs.Mount) error {
	f, err := os.OpenFile(oldInitrd, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("could not open %s: %w", oldInitrd, err)
	}
	defer f.Close()

	w := cpio.NewWriter(f)
	for _, m := range mounts {
		err = CopyFileToInitrd(w, m.Source, m.Destination)
		if err != nil {
			return fmt.Errorf("could not add file %s to initrd: %w", m.Source, err)
		}
	}
	err = w.Close()
	if err != nil {
		return fmt.Errorf("could not close initrd: %w", err)
	}

	return nil
}

// MergeFileMountsIntoInitrd adds regular-file bind mounts before the trailer
// of an uncompressed newc archive. The merged archive is written to a temporary
// file next to the initrd and replaces it only on success, so a failed merge
// leaves the initrd untouched. The caller should pass only bind mounts.
func MergeFileMountsIntoInitrd(oldInitrd string, mounts []specs.Mount) (retErr error) {
	fileMounts, err := regularFileBindMounts(mounts)
	if err != nil {
		return err
	}
	if len(fileMounts) == 0 {
		return nil
	}

	initrdFile, err := os.Open(oldInitrd)
	if err != nil {
		return fmt.Errorf("could not open %s: %w", oldInitrd, err)
	}
	defer initrdFile.Close()

	trailerOffset, entries, inodes, err := inspectInitrd(initrdFile)
	if err != nil {
		return fmt.Errorf("could not parse %s: %w", oldInitrd, err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(oldInitrd), filepath.Base(oldInitrd)+".tmp-*")
	if err != nil {
		return fmt.Errorf("could not create temporary file for %s: %w", oldInitrd, err)
	}
	defer func() {
		if retErr != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
		}
	}()

	if err := copyOwnerAndMode(initrdFile, tmpFile); err != nil {
		return fmt.Errorf("could not copy owner and mode of %s: %w", oldInitrd, err)
	}
	if err := writeMergedInitrd(tmpFile, initrdFile, trailerOffset, fileMounts, entries, inodes); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("could not close temporary file for %s: %w", oldInitrd, err)
	}
	if err := os.Rename(tmpFile.Name(), oldInitrd); err != nil {
		return fmt.Errorf("could not replace %s: %w", oldInitrd, err)
	}

	return nil
}

func copyOwnerAndMode(src, dst *os.File) error {
	info, err := src.Stat()
	if err != nil {
		return err
	}
	fileInfo := info.Sys().(*syscall.Stat_t)
	if err := dst.Chown(int(fileInfo.Uid), int(fileInfo.Gid)); err != nil {
		return err
	}
	return dst.Chmod(info.Mode().Perm())
}

// writeMergedInitrd copies the records of src up to its trailer into dst, then
// adds the file mounts and a new trailer.
func writeMergedInitrd(dst, src *os.File, trailerOffset int64, fileMounts []specs.Mount, entries map[string]cpio.FileMode, inodes *inodeAllocator) error {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("could not seek in %s: %w", src.Name(), err)
	}
	if _, err := io.CopyN(dst, src, trailerOffset); err != nil {
		return fmt.Errorf("could not copy %s: %w", src.Name(), err)
	}

	w := cpio.NewWriter(dst)
	for _, mount := range fileMounts {
		archiveName, err := archivePath(mount.Destination)
		if err != nil {
			return err
		}
		lookupName := archiveLookupPath(archiveName)
		if err := checkDestinationConflict(lookupName, entries); err != nil {
			return fmt.Errorf("could not add mount destination %s to initrd: %w", mount.Destination, err)
		}
		if err := addMissingParents(w, lookupName, entries, inodes); err != nil {
			return err
		}
		inode, err := inodes.allocate()
		if err != nil {
			return fmt.Errorf("could not allocate inode for file %s: %w", archiveName, err)
		}
		if err := copyFileToInitrdWithInode(w, mount.Source, archiveName, inode); err != nil {
			return fmt.Errorf("could not add file %s to initrd: %w", mount.Source, err)
		}
		entries[lookupName] = cpio.TypeReg
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("could not close initrd writer: %w", err)
	}

	return nil
}

// checkDestinationConflict rejects a destination the guest could not unpack: a
// file replacing a directory, or a file below an entry that cannot hold it. The
// archive itself would stay valid, so the guest would only fail later at boot.
// A symlink may point at a directory, so it is accepted as a parent.
func checkDestinationConflict(lookupName string, entries map[string]cpio.FileMode) error {
	if mode, exists := entries[lookupName]; exists && mode&cpio.ModeType == cpio.TypeDir {
		return fmt.Errorf("%s is a directory in the initrd", lookupName)
	}

	parentName := ""
	for _, component := range strings.Split(path.Dir(lookupName), "/") {
		if component == "" || component == "." {
			continue
		}
		parentName = path.Join(parentName, component)
		mode, exists := entries[parentName]
		if !exists {
			continue
		}
		if fileType := mode & cpio.ModeType; fileType != cpio.TypeDir && fileType != cpio.TypeSymlink {
			return fmt.Errorf("%s is not a directory in the initrd", parentName)
		}
	}

	return nil
}

func regularFileBindMounts(mounts []specs.Mount) ([]specs.Mount, error) {
	var fileMounts []specs.Mount
	for _, mount := range mounts {
		info, err := os.Stat(mount.Source)
		if err != nil {
			return nil, fmt.Errorf("could not stat file %s: %w", mount.Source, err)
		}
		if info.Mode().IsRegular() {
			fileMounts = append(fileMounts, mount)
		}
	}
	return fileMounts, nil
}

func inspectInitrd(f *os.File) (int64, map[string]cpio.FileMode, *inodeAllocator, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, nil, nil, err
	}

	r := cpio.NewReader(f)
	entries := make(map[string]cpio.FileMode)
	inodes := newInodeAllocator()
	var trailerOffset int64
	for {
		hdr, err := r.Next()
		if errors.Is(err, io.EOF) {
			if err := validateTrailerAt(f, trailerOffset); err != nil {
				return 0, nil, nil, err
			}
			return trailerOffset, entries, inodes, nil
		}
		if err != nil {
			return 0, nil, nil, fmt.Errorf("could not read newc record at offset %d: %w", trailerOffset, err)
		}
		entries[archiveLookupPath(hdr.Name)] = hdr.Mode
		inodes.reserve(hdr.Inode)
		if _, err := io.Copy(io.Discard, r); err != nil {
			return 0, nil, nil, err
		}
		offset, err := f.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, nil, nil, err
		}
		trailerOffset = (offset + 3) &^ 3
	}
}

func validateTrailerAt(f *os.File, offset int64) error {
	var header [newcHeaderSize]byte
	n, err := f.ReadAt(header[:], offset)
	if err != nil {
		if errors.Is(err, io.EOF) && n == 0 {
			return fmt.Errorf("missing newc trailer at offset %d: reached physical EOF", offset)
		}
		return fmt.Errorf("could not read newc trailer header at offset %d: %w", offset, err)
	}

	magic := string(header[:6])
	if magic != "070701" && magic != "070702" {
		return fmt.Errorf("invalid newc trailer at offset %d: unsupported magic %q", offset, magic)
	}

	size, err := strconv.ParseUint(string(header[54:62]), 16, 32)
	if err != nil {
		return fmt.Errorf("invalid newc trailer at offset %d: invalid file size: %w", offset, err)
	}
	if size != 0 {
		return fmt.Errorf("invalid newc trailer at offset %d: file size is %d, not zero", offset, size)
	}

	nameSize, err := strconv.ParseUint(string(header[94:102]), 16, 32)
	if err != nil {
		return fmt.Errorf("invalid newc trailer at offset %d: invalid name size: %w", offset, err)
	}
	expectedName := trailerName + "\x00"
	if nameSize != uint64(len(expectedName)) {
		return fmt.Errorf("invalid newc trailer at offset %d: name size is %d, not %d", offset, nameSize, len(expectedName))
	}

	name := make([]byte, len(expectedName))
	if _, err := f.ReadAt(name, offset+newcHeaderSize); err != nil {
		return fmt.Errorf("could not read newc trailer name at offset %d: %w", offset, err)
	}
	if string(name) != expectedName {
		return fmt.Errorf("invalid newc trailer at offset %d: name is not %q", offset, trailerName)
	}

	return nil
}

func archivePath(destination string) (string, error) {
	if !path.IsAbs(destination) {
		return "", fmt.Errorf("initrd mount destination %q is not absolute", destination)
	}
	// The supported Unikraft image flow uses the ./ namespace. Keeping all new
	// records there also makes parent lookup and later-record replacement consistent.
	return "./" + archiveLookupPath(destination), nil
}

func archiveLookupPath(name string) string {
	return strings.TrimPrefix(path.Clean(name), "/")
}

func addMissingParents(w *cpio.Writer, lookupName string, entries map[string]cpio.FileMode, inodes *inodeAllocator) error {
	parentName := ""
	for _, component := range strings.Split(path.Dir(lookupName), "/") {
		if component == "" || component == "." {
			continue
		}
		parentName = path.Join(parentName, component)
		if _, exists := entries[parentName]; exists {
			continue
		}

		archiveName := "./" + parentName
		inode, err := inodes.allocate()
		if err != nil {
			return fmt.Errorf("could not allocate inode for directory %s: %w", archiveName, err)
		}
		// Two is the conventional minimum link count for a directory. Unique
		// allocated inodes keep Unikraft from treating unrelated entries as hard links.
		hdr := &cpio.Header{Name: archiveName, Inode: inode, Mode: cpio.TypeDir | 0o755, Links: 2}
		if err := w.WriteHeader(hdr); err != nil {
			return fmt.Errorf("could not add directory %s to initrd: %w", archiveName, err)
		}
		entries[parentName] = hdr.Mode
	}
	return nil
}

func AddFileToInitrd(oldInitrd string, data string, name string) error {
	f, err := os.OpenFile(oldInitrd, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("could not open %s: %w", oldInitrd, err)
	}
	defer f.Close()

	w := cpio.NewWriter(f)
	fileInfo := syscall.Stat_t{
		Mode: syscall.S_IFREG | 0400,
		Size: int64(len(data)),
		Mtim: syscall.Timespec{Sec: time.Now().Unix()},
		Uid:  0,
		Gid:  0,
	}
	err = addInitrdRecord(w, []byte(data), &fileInfo, name, 0)
	if err != nil {
		return fmt.Errorf("could not add file %s to initrd: %w", name, err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("could not close initrd: %w", err)
	}

	return nil
}
