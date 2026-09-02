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
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cavaliergopher/cpio"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
)

func TestMergeFileMountsIntoInitrd(t *testing.T) {
	dir := t.TempDir()
	initrdPath := filepath.Join(dir, "initrd.cpio")
	writeArchive(t, initrdPath, []archiveEntry{
		{name: "./etc", inode: 42, links: 2, mode: cpio.TypeDir | 0o755},
		{name: "./etc/original", inode: 43, links: 1, mode: cpio.TypeReg | 0o644, content: "old"},
	})
	require.NoError(t, os.Chmod(initrdPath, 0o640))
	original := readFile(t, initrdPath)
	originalInfo, err := os.Stat(initrdPath)
	require.NoError(t, err)
	originalTrailer := bytes.Index(original, []byte("TRAILER!!!\x00")) - 110
	require.GreaterOrEqual(t, originalTrailer, 0)

	configSource := writeSource(t, dir, "config", "mounted config")
	replacementSource := writeSource(t, dir, "replacement", "new")
	mounts := []specs.Mount{
		{Type: "bind", Source: configSource, Destination: "/etc/app/config"},
		{Type: "bind", Source: replacementSource, Destination: "/etc/original"},
	}

	require.NoError(t, MergeFileMountsIntoInitrd(initrdPath, mounts))

	updated := readFile(t, initrdPath)
	require.Equal(t, original[:originalTrailer], updated[:originalTrailer], "existing records changed")
	require.Equal(t, 1, bytes.Count(updated, []byte("TRAILER!!!\x00")))

	entries := readArchive(t, initrdPath)
	require.Equal(t, 1, countName(entries, "./etc"), "existing parent was duplicated")
	require.Equal(t, 1, countName(entries, "./etc/app"), "missing parent was not added once")
	require.Equal(t, "mounted config", lastContent(entries, "./etc/app/config"))
	require.Equal(t, 2, countName(entries, "./etc/original"))
	require.Equal(t, "new", lastContent(entries, "./etc/original"))

	info, err := os.Stat(initrdPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(originalInfo, info), "initrd was replaced")
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestMergeFileMountsIntoInitrdCanonicalizesExistingNames(t *testing.T) {
	dir := t.TempDir()
	initrdPath := filepath.Join(dir, "initrd.cpio")
	writeArchive(t, initrdPath, []archiveEntry{
		{name: "etc", inode: 42, links: 2, mode: cpio.TypeDir | 0o755},
		{name: "etc/original", inode: 43, links: 1, mode: cpio.TypeReg | 0o644, content: "old"},
	})
	original := readFile(t, initrdPath)
	originalTrailer := bytes.Index(original, []byte("TRAILER!!!\x00")) - 110
	require.GreaterOrEqual(t, originalTrailer, 0)

	configSource := writeSource(t, dir, "config", "mounted config")
	replacementSource := writeSource(t, dir, "replacement", "new")
	require.NoError(t, MergeFileMountsIntoInitrd(initrdPath, []specs.Mount{
		{Type: "bind", Source: configSource, Destination: "/etc/app/config"},
		{Type: "bind", Source: replacementSource, Destination: "/etc/original"},
	}))

	updated := readFile(t, initrdPath)
	require.Equal(t, original[:originalTrailer], updated[:originalTrailer], "existing records changed")

	entries := readArchive(t, initrdPath)
	require.Equal(t, 1, countName(entries, "etc"), "existing parent changed")
	require.Zero(t, countName(entries, "./etc"), "equivalent parent was duplicated")
	require.Equal(t, 1, countName(entries, "./etc/app"), "missing parent was not added once")
	require.Equal(t, "mounted config", lastContent(entries, "./etc/app/config"))
	require.Equal(t, "old", lastContent(entries, "etc/original"), "existing file changed")
	require.Equal(t, 1, countName(entries, "./etc/original"), "replacement was not emitted")
	require.Equal(t, "new", lastContent(entries, "./etc/original"))
	require.Equal(t, "new", lastContentAtLookupPath(entries, "etc/original"),
		"mounted file did not replace the equivalent existing path")
}

func TestAddMissingParentsWalksTopDownAndTerminatesAtRoot(t *testing.T) {
	for _, lookupName := range []string{"etc/app/config", "/etc/app/config"} {
		t.Run(lookupName, func(t *testing.T) {
			initrdPath := filepath.Join(t.TempDir(), "initrd.cpio")
			archive, err := os.Create(initrdPath)
			require.NoError(t, err)
			w := cpio.NewWriter(archive)

			require.NoError(t, addMissingParents(w, lookupName, make(map[string]struct{}), newInodeAllocator()))
			require.NoError(t, w.Close())
			require.NoError(t, archive.Close())

			entries := readArchive(t, initrdPath)
			require.Len(t, entries, 2)
			require.Equal(t, "./etc", entries[0].name)
			require.Equal(t, "./etc/app", entries[1].name)
			require.Equal(t, 2, entries[0].links)
			require.Equal(t, 2, entries[1].links)
		})
	}
}

func TestMergeFileMountsIntoInitrdAssignsUnusedInodes(t *testing.T) {
	dir := t.TempDir()
	initrdPath := filepath.Join(dir, "initrd.cpio")
	writeArchive(t, initrdPath, []archiveEntry{
		{name: "./existing-one", inode: 1, links: 2, mode: cpio.TypeDir | 0o755},
		{name: "./existing-two", inode: 2, links: 2, mode: cpio.TypeDir | 0o755},
	})
	source := writeSource(t, dir, "mounted", "new")

	require.NoError(t, MergeFileMountsIntoInitrd(initrdPath, []specs.Mount{
		{Type: "bind", Source: source, Destination: "/new/child/file"},
	}))

	existingInodes := map[int64]struct{}{1: {}, 2: {}}
	insertedNames := map[string]struct{}{
		"./new":            {},
		"./new/child":      {},
		"./new/child/file": {},
	}
	insertedInodes := make(map[int64]string)
	for _, entry := range readArchive(t, initrdPath) {
		if _, inserted := insertedNames[entry.name]; !inserted {
			continue
		}
		require.NotZero(t, entry.inode)
		require.NotContains(t, existingInodes, entry.inode,
			"inserted entry %s reused an existing inode", entry.name)
		require.NotContains(t, insertedInodes, entry.inode,
			"inserted entries %s and %s share an inode", insertedInodes[entry.inode], entry.name)
		insertedInodes[entry.inode] = entry.name
	}
	require.Len(t, insertedInodes, len(insertedNames))
}

func TestCopyFileMountsToInitrdStillAppendsArchive(t *testing.T) {
	dir := t.TempDir()
	initrdPath := filepath.Join(dir, "initrd.cpio")
	writeArchive(t, initrdPath, []archiveEntry{{name: "./original", mode: cpio.TypeReg | 0o644, content: "old"}})
	original := readFile(t, initrdPath)
	source := writeSource(t, dir, "mounted", "new")

	require.NoError(t, CopyFileMountsToInitrd(initrdPath, []specs.Mount{
		{Type: "bind", Source: source, Destination: "/mounted"},
	}))

	updated := readFile(t, initrdPath)
	require.Equal(t, original, updated[:len(original)])
	require.Equal(t, 2, bytes.Count(updated, []byte("TRAILER!!!\x00")))
	require.Equal(t, 0, countName(readArchive(t, initrdPath), "/mounted"), "first member unexpectedly exposed appended entry")
}

func TestMergeFileMountsIntoInitrdWithoutRegularBindMountsDoesNotRewrite(t *testing.T) {
	dir := t.TempDir()
	initrdPath := filepath.Join(dir, "initrd.cpio")
	writeArchive(t, initrdPath, []archiveEntry{{name: "./original", mode: cpio.TypeReg | 0o644, content: "old"}})
	original := readFile(t, initrdPath)
	originalInfo, err := os.Stat(initrdPath)
	require.NoError(t, err)

	require.NoError(t, MergeFileMountsIntoInitrd(initrdPath, []specs.Mount{
		{Type: "tmpfs", Source: "tmpfs", Destination: "/tmp"},
		{Type: "bind", Source: dir, Destination: "/directory"},
	}))

	require.Equal(t, original, readFile(t, initrdPath))
	updatedInfo, err := os.Stat(initrdPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(originalInfo, updatedInfo), "initrd was replaced")
}

func TestMergeFileMountsIntoInitrdParseFailureDoesNotMutateOriginal(t *testing.T) {
	dir := t.TempDir()
	initrdPath := filepath.Join(dir, "initrd.cpio")
	require.NoError(t, os.WriteFile(initrdPath, []byte("not a cpio archive"), 0o600))
	original := readFile(t, initrdPath)
	originalInfo, err := os.Stat(initrdPath)
	require.NoError(t, err)
	source := writeSource(t, dir, "mounted", "new")

	err = MergeFileMountsIntoInitrd(initrdPath, []specs.Mount{
		{Type: "bind", Source: source, Destination: "/mounted"},
	})

	require.Error(t, err)
	require.Equal(t, original, readFile(t, initrdPath))
	updatedInfo, err := os.Stat(initrdPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(originalInfo, updatedInfo), "initrd was replaced")
}

func TestMergeFileMountsIntoInitrdInvalidTrailerDoesNotMutateOriginal(t *testing.T) {
	tests := map[string]func(archive []byte, trailerOffset int) []byte{
		"missing trailer": func(archive []byte, trailerOffset int) []byte {
			return archive[:trailerOffset]
		},
		"empty initrd": func(_ []byte, _ int) []byte {
			return nil
		},
		"truncated trailer": func(archive []byte, trailerOffset int) []byte {
			return archive[:trailerOffset+newcHeaderSize+len(trailerName)/2]
		},
		"non-zero trailer size": func(archive []byte, trailerOffset int) []byte {
			copy(archive[trailerOffset+54:trailerOffset+62], "00000001")
			return archive
		},
		"invalid trailer name field": func(archive []byte, trailerOffset int) []byte {
			archive[trailerOffset+newcHeaderSize+len(trailerName)] = 'X'
			return archive
		},
	}

	for name, corruptArchive := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			initrdPath := filepath.Join(dir, "initrd.cpio")
			writeArchive(t, initrdPath, []archiveEntry{
				{name: "./original", mode: cpio.TypeReg | 0o644, content: "old"},
			})
			archive := readFile(t, initrdPath)
			trailerOffset := bytes.Index(archive, []byte(trailerName+"\x00")) - newcHeaderSize
			require.GreaterOrEqual(t, trailerOffset, 0)
			require.NoError(t, os.WriteFile(initrdPath, corruptArchive(archive, trailerOffset), 0o600))

			original := readFile(t, initrdPath)
			originalInfo, err := os.Stat(initrdPath)
			require.NoError(t, err)
			source := writeSource(t, dir, "mounted", "new")

			err = MergeFileMountsIntoInitrd(initrdPath, []specs.Mount{
				{Type: "bind", Source: source, Destination: "/mounted"},
			})

			require.Error(t, err)
			require.Equal(t, original, readFile(t, initrdPath))
			updatedInfo, err := os.Stat(initrdPath)
			require.NoError(t, err)
			require.True(t, os.SameFile(originalInfo, updatedInfo), "initrd was replaced")
		})
	}
}

type archiveEntry struct {
	name    string
	inode   int64
	links   int
	mode    cpio.FileMode
	content string
}

func writeArchive(t *testing.T, archivePath string, entries []archiveEntry) {
	t.Helper()
	f, err := os.Create(archivePath)
	require.NoError(t, err)
	w := cpio.NewWriter(f)
	for _, entry := range entries {
		hdr := &cpio.Header{
			Name: entry.name, Inode: entry.inode, Links: entry.links,
			Mode: entry.mode, Size: int64(len(entry.content)),
		}
		require.NoError(t, w.WriteHeader(hdr))
		_, err := w.Write([]byte(entry.content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
}

func readArchive(t *testing.T, archivePath string) []archiveEntry {
	t.Helper()
	f, err := os.Open(archivePath)
	require.NoError(t, err)
	defer f.Close()

	var entries []archiveEntry
	r := cpio.NewReader(f)
	for {
		hdr, err := r.Next()
		if err == io.EOF {
			return entries
		}
		require.NoError(t, err)
		content, err := io.ReadAll(r)
		require.NoError(t, err)
		entries = append(entries, archiveEntry{
			name: hdr.Name, inode: hdr.Inode, links: hdr.Links,
			mode: hdr.Mode, content: string(content),
		})
	}
}

func writeSource(t *testing.T, dir, name, content string) string {
	t.Helper()
	filename := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(filename, []byte(content), 0o600))
	return filename
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(name)
	require.NoError(t, err)
	return content
}

func countName(entries []archiveEntry, name string) int {
	count := 0
	for _, entry := range entries {
		if entry.name == name {
			count++
		}
	}
	return count
}

func lastContent(entries []archiveEntry, name string) string {
	var content string
	for _, entry := range entries {
		if entry.name == name {
			content = entry.content
		}
	}
	return content
}

func lastContentAtLookupPath(entries []archiveEntry, lookupPath string) string {
	var content string
	for _, entry := range entries {
		if archiveLookupPath(entry.name) == lookupPath {
			content = entry.content
		}
	}
	return content
}
