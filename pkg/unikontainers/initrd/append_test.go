// Copyright (c) 2023-2026, Nubificus LTD
// SPDX-License-Identifier: Apache-2.0

package initrd

import (
	"os"
	"strings"
	"testing"
)

func TestAddFileToInitrdIsPlatformNeutral(t *testing.T) {
	path := t.TempDir() + "/initrd"
	if err := os.WriteFile(path, []byte("base-cpio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AddFileToInitrd(path, "/bin/echo\nhello world\n", "/urunc-cmd"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"base-cpio", "urunc-cmd", "/bin/echo", "hello world"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("appended initrd does not contain %q", want)
		}
	}
}
