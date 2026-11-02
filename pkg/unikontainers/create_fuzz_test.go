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

package unikontainers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cavaliergopher/cpio"
	specs "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/urunc-dev/urunc/internal/fuzzseed"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// FuzzCreate takes a bundle through create and start, up to the monitor's command line. The
// input is config.json, then optionally a NUL and urunc.json, then optionally a NUL and a host byte.
func FuzzCreate(f *testing.F) {
	fuzzseed.QuietLogs(f)
	env := newFuzzEnv(f)
	for _, seed := range fuzzCreateSeeds() {
		fuzzseed.Add(f, seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		first := env.run(t, data)
		if second := env.run(t, data); !reflect.DeepEqual(first, second) {
			t.Fatalf("the same input ended differently: %+v, then %+v", first, second)
		}
	})
}

// fuzzBoot is what an image ships as its kernel, initrd or block image: a cpio archive with one
// file, since a real image is never empty and urunc merges file mounts into it.
func fuzzBoot(f *testing.F) []byte {
	var boot bytes.Buffer
	w := cpio.NewWriter(&boot)
	if err := w.WriteHeader(&cpio.Header{Name: "boot", Size: 4, Mode: 0o644}); err != nil {
		f.Fatal(err)
	}
	if _, err := w.Write([]byte("boot")); err != nil {
		f.Fatal(err)
	}
	if err := w.Close(); err != nil {
		f.Fatal(err)
	}
	return boot.Bytes()
}

// fuzzEnv is what stays the same for every input.
type fuzzEnv struct {
	base, bundle, rootfs, state string
	hosts                       map[byte]fuzzHost
	boot                        []byte
}

// sharedFuzzEnv is built once per process: under libFuzzer the fuzz function is called for every
// input, and its f.TempDir is removed after each of them.
var sharedFuzzEnv *fuzzEnv

func newFuzzEnv(f *testing.F) *fuzzEnv {
	path := os.Getenv("PATH")
	f.Cleanup(func() { _ = os.Setenv("PATH", path); _ = os.Unsetenv(UruncConfigFileEnv) })
	if e := sharedFuzzEnv; e != nil {
		if _, err := os.Stat(e.base); err == nil {
			return e
		}
	}
	base, err := os.MkdirTemp("", "urunc-fuzz-")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = os.RemoveAll(base) })
	fuzzDevices(f, base)
	bundle := filepath.Join(base, "bundle")
	sharedFuzzEnv = &fuzzEnv{base: base, bundle: bundle, rootfs: filepath.Join(bundle, "rootfs"),
		state: filepath.Join(base, "state"), hosts: map[byte]fuzzHost{}, boot: fuzzBoot(f)}
	return sharedFuzzEnv
}

// The stages an input of FuzzCreate goes through, in order.
const (
	stageNew    = "New"
	stageRootfs = "rootfs"
	stageSaved  = "saved"
	stageStart  = "start"
	stageArgv   = "argv" // the monitor's command was built
)

// fuzzOutcome is how far an input got, what create saved for the monitor process, and what the
// monitor would have been run with.
type fuzzOutcome struct {
	stage    string
	command  string
	argv     []string
	fcConfig []byte
	res      monitorResources
}

// fuzzRun is what one input of FuzzCreate left behind.
type fuzzRun struct {
	fuzzOutcome
	host byte
}

// run takes one input through the pipeline.
func (e *fuzzEnv) run(t *testing.T, data []byte) fuzzOutcome {
	r := &fuzzRun{}
	e.pipeline(t, r, data)
	return r.fuzzOutcome
}

func (e *fuzzEnv) pipeline(t *testing.T, r *fuzzRun, data []byte) {
	// One input split at NULs, which JSON cannot contain, so libFuzzer seeds keep every part whole.
	parts := bytes.SplitN(data, []byte{0}, 3)
	var uruncJSON []byte
	if len(parts) > 1 {
		uruncJSON = parts[1]
	}
	if len(parts) > 2 && len(parts[2]) > 0 {
		r.host = parts[2][0]
	}
	// "@BUNDLE@" stands for the bundle's path, where bind-mount sources live.
	config := bytes.ReplaceAll(parts[0], []byte("@BUNDLE@"), []byte(e.bundle))
	if !writeFuzzBundle(t, e.bundle, config, uruncJSON, r.host) {
		return
	}
	r.stage = stageNew
	cfg := e.host(t, r.host)
	u, err := New(e.bundle, "fuzz", e.state, cfg)
	if err != nil {
		return
	}
	ann := u.State.Annotations
	guest, err := unikernels.New(ann[annotType])
	if err != nil {
		t.Fatalf("New accepted guest %q that cannot be built: %v", ann[annotType], err)
	}
	vmm, err := hypervisors.NewVMM(hypervisors.VmmType(ann[annotHypervisor]), cfg.Monitors)
	if errors.Is(err, hypervisors.ErrVMMNotInstalled) {
		return
	}
	if err != nil {
		t.Fatalf("New accepted monitor %q that cannot be built: %v", ann[annotHypervisor], err)
	}

	// The image ships the files its annotations name.
	for _, k := range []string{annotBinary, annotInitrd, annotBlock} {
		p := filepath.Join(e.rootfs, filepath.Clean("/"+ann[k]))
		if ann[k] != "" && os.MkdirAll(filepath.Dir(p), 0o755) == nil {
			_ = os.WriteFile(p, e.boot, 0o600)
		}
	}
	r.stage = stageRootfs
	rp, err := ChooseRootfs(e.bundle, u.Spec.Root.Path, ann, cfg)
	if err != nil || (rp.Type == "block" && rp.MountedPath != "") { // mounting a block rootfs needs root
		return
	}
	mon := cfg.Monitors[ann[annotHypervisor]]
	rfs, err := u.newRootfsBuilder(e.rootfs, rp, guest, ann[annotBinary], ann[annotInitrd],
		monitorMemoryBytes(mon.DefaultMemoryMB, u.Spec.Linux.Resources))
	if err != nil || rfs.preSetup() != nil {
		return
	}
	res, err := getMonitorResources(rfs, rp, vmm, mon.DataPath, u.Spec.Mounts, filepath.Join(e.base, "dev"))
	if err != nil || rfs.postSetup() != nil {
		return
	}
	res.Rootfs = rp
	checkMonitorResources(t, e.base, res)
	checkMonitorCapabilities(t, u.Spec, res.TUNTAP)

	// What create saves for the monitor process: BuildContainerConfig reads the resources back.
	r.stage = stageSaved
	for _, dir := range []string{u.BaseDir, rp.MonRootfs} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveMonitorResources(u.BaseDir, res); err != nil {
		t.Fatal(err)
	}
	checkConfinement(t, u, containerConfig(u), e.bundle)
	if err := u.writeMonitorSpec(rp, res); err != nil {
		t.Fatal(err)
	}
	if err := u.saveContainerState(); err != nil {
		t.Fatal(err)
	}

	r.res = res

	// What start and the monitor process do with it.
	if _, err = Get("fuzz", u.RootDir); err != nil {
		t.Fatalf("start cannot load what create saved: %v", err)
	}
	ms, err := LoadMonitorSpec(rp.MonRootfs)
	if err != nil {
		t.Fatalf("the monitor process cannot load its spec: %v", err)
	}
	checkMonitorSpec(t, u.Spec, ms)
	r.stage = stageStart
	st, err := startMonitor(ms, r.host)
	if err != nil {
		return
	}
	r.stage, r.command, r.argv, r.fcConfig = stageArgv, st.ms.ExecArgs.Command, st.argv, st.fcConfig
	checkMonitorStart(t, u.Spec, st.ms, rp.Type, r.host, st.argv)
}

// fuzzDevices makes the monitor's devices links to devices every host and container has, so
// that create does not depend on the host having /dev/kvm or /dev/net/tun.
func fuzzDevices(f *testing.F, base string) {
	dev := filepath.Join(base, "dev")
	if err := os.MkdirAll(filepath.Join(dev, "net"), 0o755); err != nil {
		f.Fatal(err)
	}
	for name, target := range map[string]string{"null": "/dev/null", "urandom": "/dev/urandom", "net/tun": "/dev/null", "kvm": "/dev/null"} {
		if err := os.Symlink(target, filepath.Join(dev, name)); err != nil {
			f.Fatal(err)
		}
	}
}

// fuzzNameserver is the nameserver of the usable resolv.conf files below.
const fuzzNameserver = "9.9.9.9"

// fuzzResolv is what the engine left in resolv.conf, by the top two bits of the host
// byte: a usable file, one with no IPv4 nameserver, no file, and a usable file again.
var fuzzResolv = [4][]byte{
	[]byte("nameserver " + fuzzNameserver + "\n"), []byte("# local\nsearch lan\nnameserver ::1\n"), nil,
	[]byte("nameserver " + fuzzNameserver + "\n"),
}

// monitorBinaries are what urunc looks for on PATH; qemu's name ends in the architecture.
var monitorBinaries = map[hypervisors.VmmType]string{
	hypervisors.QemuVmm:            hypervisors.QemuBinary + map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH],
	hypervisors.HvtVmm:             hypervisors.HvtBinary,
	hypervisors.SptVmm:             hypervisors.SptBinary,
	hypervisors.FirecrackerVmm:     hypervisors.FirecrackerBinary,
	hypervisors.CloudHypervisorVmm: hypervisors.CloudHypervisorBinary,
	hypervisors.HyperlightVmm:      hypervisors.HyperlightBinary,
}

// fuzzHost is a host the fuzzer can choose: urunc's config for it and the PATH that goes with it.
type fuzzHost struct {
	cfg  *UruncConfig
	path string
}

// host returns the config of the host that the host byte describes, and puts its PATH in place:
// vAccel (bit 2), virtiofsd (bit 3), the monitors in the config, on PATH, missing or a broken
// config (bits 4-5), vhost networking (bits 6-7 set).
func (e *fuzzEnv) host(t *testing.T, choice byte) *UruncConfig {
	key := choice &^ 3 // the network does not change the host
	h, ok := e.hosts[key]
	if !ok {
		h = newFuzzHost(t, filepath.Join(e.base, "host", strconv.Itoa(int(key))), e.base, key)
		e.hosts[key] = h
	}
	if err := os.Setenv("PATH", h.path); err != nil {
		t.Fatal(err)
	}
	return h.cfg
}

func newFuzzHost(t *testing.T, dir, base string, key byte) fuzzHost {
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := func(name string) {
		// urunc finds executables with exec.LookPath, which requires the execute bit.
		if err := os.WriteFile(filepath.Join(bin, name), nil, 0o755); err != nil { //nolint:gosec
			t.Fatal(err)
		}
	}
	where := key >> 4 & 3
	conf := fmt.Sprintf("[runtime]\nvAccel = %t\n[extra_binaries.virtiofsd]\npath = %q\n", key&4 != 0, filepath.Join(bin, "virtiofsd"))
	if key&8 == 0 {
		stub("virtiofsd")
	}
	for vmm, binary := range monitorBinaries {
		if where != 2 {
			stub(binary)
		}
		if where == 0 {
			conf += fmt.Sprintf("[monitors.%q]\npath = %q\ndata_path = %q\nvhost = %t\n", vmm, filepath.Join(bin, binary), base, key>>6 == 3)
		}
	}
	if where == 3 {
		conf = "[runtime\n" // not TOML: urunc uses its defaults
	}
	file := filepath.Join(dir, "urunc.toml")
	if err := os.WriteFile(file, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv(UruncConfigFileEnv, file); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadUruncConfig(ResolveUruncConfigPath()) // the defaults, for a broken file
	_, _ = ParseLogMetricsConfig(file)
	_, _ = ParseLogMetricsConfig(file + ".missing") // most hosts have no config file at all
	return fuzzHost{cfg: cfg, path: bin}
}

// writeFuzzBundle writes the bundle the way an engine would, or returns false for a config.json
// no engine writes. The top two bits of host choose what the engine left in the bind sources.
func writeFuzzBundle(t *testing.T, bundle string, config, uruncJSON []byte, host byte) bool {
	var spec specs.Spec
	if json.Unmarshal(config, &spec) != nil || spec.Root == nil || spec.Root.Path != "rootfs" || spec.Process == nil {
		return false
	}
	if err := os.RemoveAll(bundle); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(bundle, "rootfs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, m := range spec.Mounts {
		if m.Type != "bind" || m.Source == "/proc" {
			continue
		}
		if filepath.Dir(m.Source) != bundle || (fuzzResolv[host>>6] != nil && os.WriteFile(m.Source, fuzzResolv[host>>6], 0o600) != nil) {
			return false
		}
	}
	if err := os.WriteFile(filepath.Join(bundle, configFilename), config, 0o600); err != nil {
		t.Fatal(err)
	}
	if len(uruncJSON) > 0 {
		if err := os.WriteFile(filepath.Join(bundle, "rootfs", uruncJSONFilename), uruncJSON, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return true
}

// monitorHostPaths are the host paths a monitor is given besides the container's files: the
// libraries, QEMU's data and the default virtiofsd.
var monitorHostPaths = sync.OnceValue(func() []string {
	paths := []string{"/proc", "/lib", "/lib64", "/usr/lib", "/usr/libexec/virtiofsd"}
	for _, dir := range []string{"qemu", "seabios"} {
		if path, err := findQemuDataDir(dir); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
})

// checkMonitorResources fails if the monitor is given a host path or device
// beyond the container's own files, its binaries and data, and host libraries.
func checkMonitorResources(t *testing.T, base string, res monitorResources) {
	for _, m := range res.Mounts {
		src := filepath.Clean(m.Source)
		if m.Type == "bind" && !strings.HasPrefix(src, base+"/") && !slices.Contains(monitorHostPaths(), src) {
			t.Fatalf("monitor gets host path %s", src)
		}
	}
	for _, d := range res.Devices {
		if rel, err := filepath.Rel(filepath.Join(base, "dev"), d.Path); err != nil || !slices.Contains([]string{"null", "urandom", "net/tun", "kvm"}, rel) {
			t.Fatalf("monitor gets host device %s", d.Path)
		}
	}
}

// checkMonitorCapabilities fails if the monitor gets a capability the container
// did not ask for, besides the CAP_NET_ADMIN a monitor with a tap device needs.
func checkMonitorCapabilities(t *testing.T, spec *specs.Spec, tuntap bool) {
	want := spec.Process.Capabilities
	if want == nil {
		return
	}
	got := monitorCapabilities(spec, tuntap)
	for _, sets := range [][2][]string{{got.Bounding, want.Bounding}, {got.Effective, want.Effective},
		{got.Permitted, want.Permitted}, {got.Inheritable, want.Inheritable}, {got.Ambient, want.Ambient}} {
		for _, c := range sets[0] {
			if (c != "CAP_NET_ADMIN" || !tuntap) && !slices.Contains(sets[1], c) {
				t.Fatalf("monitor gets %s, which the container did not ask for", c)
			}
		}
	}
}
