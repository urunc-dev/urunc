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
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// These tests have no build tag: the script runs on macOS hosts as well, and
// the host is where it is run to build the initrd for hull and brig.

// buildScript is packaging/container-initrd/build-container-initrd.sh, which
// produces the container boot initrd. These tests run it with stub binaries
// and check the archive it writes.
const buildScript = "../../../packaging/container-initrd/build-container-initrd.sh"

func runBuildScript(t *testing.T, env ...string) (string, string) {
	t.Helper()
	for _, tool := range []string{"bash", "cpio"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "container-initrd")
	cmd := exec.Command("bash", buildScript, out)
	cmd.Env = append(os.Environ(), env...)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-container-initrd.sh: %v\n%s", err, msg)
	}
	return out, dir
}

// listArchive returns the entries of a cpio archive, a symlink as
// "name -> target".
func listArchive(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cmd := exec.Command("cpio", "-tv")
	cmd.Stdin = f
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("cpio -tv: %v", err)
	}
	var entries []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		// The listing ends with the name, or with "name -> target".
		switch {
		case len(fields) >= 3 && fields[len(fields)-2] == "->":
			entries = append(entries, strings.TrimPrefix(fields[len(fields)-3], "./")+" -> "+fields[len(fields)-1])
		case len(fields) > 0:
			entries = append(entries, strings.TrimPrefix(fields[len(fields)-1], "./"))
		}
	}
	slices.Sort(entries)
	return entries
}

func writeStub(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("stub "+name+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildScriptSingleLayout(t *testing.T) {
	stub := writeStub(t, t.TempDir(), "urunit")
	out, _ := runBuildScript(t, "URUNIT="+stub)
	got := listArchive(t, out)
	want := []string{"dev", "init -> urunit", "newroot", "proc", "run", "sys", "urunit", "vz-init -> urunit"}
	if !slices.Equal(got, want) {
		t.Errorf("single layout = %v, want %v", got, want)
	}
	// No BusyBox and no separate agent in the new layout.
	for _, e := range got {
		if strings.Contains(e, "busybox") || strings.Contains(e, "urunit-agent") {
			t.Errorf("single layout carries %q", e)
		}
	}
}

func TestBuildScriptLegacyLayout(t *testing.T) {
	d := t.TempDir()
	out, _ := runBuildScript(t,
		"INITRD_LAYOUT=legacy",
		"URUNIT="+writeStub(t, d, "urunit"),
		"BUSYBOX="+writeStub(t, d, "busybox-stub"),
		"URUNIT_AGENT="+writeStub(t, d, "agent-stub"))
	got := listArchive(t, out)
	want := []string{"busybox", "dev", "init", "newroot", "proc", "run", "sys", "urunit", "urunit-agent", "vz-init"}
	if !slices.Equal(got, want) {
		t.Errorf("legacy layout = %v, want %v", got, want)
	}
}

// TestBuildScriptPlatform checks that URUNIT unset builds the binary through
// build-urunit.sh and the container tool, for the architecture TARGET_ARCH
// names. URUNIT_SRC keeps the build off the network. The fake tool records
// its arguments and drops a stub into the output mount.
func TestBuildScriptPlatform(t *testing.T) {
	cases := []struct{ arch, platform string }{
		{"amd64", "linux/amd64"},
		{"x86_64", "linux/amd64"},
		{"aarch64", "linux/arm64"},
		{"arm64", "linux/arm64"},
	}
	for _, tc := range cases {
		t.Run(tc.arch, func(t *testing.T) {
			d := t.TempDir()
			args := filepath.Join(d, "args")
			fake := filepath.Join(d, "fake-podman")
			script := `#!/bin/sh
printf '%s\n' "$@" > "` + args + `"
for a in "$@"; do
  if [ "${a%:/out}" != "$a" ]; then printf stub > "${a%:/out}/urunit"; fi
done
`
			if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			out, _ := runBuildScript(t, "CNTR_TOOL="+fake, "TARGET_ARCH="+tc.arch, "URUNIT_SRC="+t.TempDir())
			raw, err := os.ReadFile(args)
			if err != nil {
				t.Fatalf("the container tool was not run: %v", err)
			}
			a := strings.Split(strings.TrimSpace(string(raw)), "\n")
			i := slices.Index(a, "--platform")
			if i < 0 || i+1 >= len(a) || a[i+1] != tc.platform {
				t.Errorf("arguments = %v, want --platform %s", a, tc.platform)
			}
			if got := listArchive(t, out); !slices.Contains(got, "vz-init -> urunit") {
				t.Errorf("archive = %v, want the vz-init symlink", got)
			}
		})
	}
}

func TestBuildScriptRejects(t *testing.T) {
	stub := writeStub(t, t.TempDir(), "urunit")
	cases := []struct {
		name string
		env  []string
	}{
		{"unknown layout", []string{"INITRD_LAYOUT=bogus", "URUNIT=" + stub}},
		{"unknown arch", []string{"TARGET_ARCH=riscv64", "URUNIT=" + stub}},
		{"legacy without urunit", []string{"INITRD_LAYOUT=legacy", "URUNIT="}},
		{"missing urunit file", []string{"URUNIT=/nonexistent/urunit"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exec.LookPath("cpio"); err != nil {
				t.Skip("cpio is not installed")
			}
			out := filepath.Join(t.TempDir(), "initrd")
			cmd := exec.Command("bash", buildScript, out)
			cmd.Env = append(os.Environ(), tc.env...)
			if msg, err := cmd.CombinedOutput(); err == nil {
				t.Errorf("the script succeeded, output:\n%s", msg)
			}
			if _, err := os.Stat(out); err == nil {
				t.Errorf("the script left an archive behind")
			}
		})
	}
}
