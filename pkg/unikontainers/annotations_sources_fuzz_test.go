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
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// FuzzAnnotationSources checks that the same
// unikernel settings, given once as spec annotations and once as the image's urunc.json,
// must be accepted or rejected alike, give the same config and pass the same value checks.
func FuzzAnnotationSources(f *testing.F) {
	f.Add("linux", "qemu", "/.boot/kernel", "", "/.boot/initrd", "", "", "true", "", "")
	f.Add("rumprun", "hvt", "/unikernel/redis.hvt", "", "", "/data.img", "/data", "", "tap", "storage")
	empty := f.TempDir()

	f.Fuzz(func(t *testing.T, guest, monitor, binary, version, initrd, block, blkMnt, mountRootfs, netDev, blkDev string) {
		ann := map[string]string{
			annotType: guest, annotHypervisor: monitor, annotBinary: binary, annotVersion: version,
			annotInitrd: initrd, annotBlock: block, annotBlockMntPoint: blkMnt, annotMountRootfs: mountRootfs,
			annotNetDev: netDev, annotBlkDev: blkDev,
		}
		fromSpec, errSpec := GetUnikernelConfig("", &specs.Spec{Root: &specs.Root{Path: empty}, Annotations: ann})

		enc := map[string]string{}
		for k, v := range ann {
			enc[k] = base64.StdEncoding.EncodeToString([]byte(v))
		}
		raw, _ := json.Marshal(enc)
		rootfs := t.TempDir()
		if err := os.WriteFile(filepath.Join(rootfs, uruncJSONFilename), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		fromJSON, errJSON := GetUnikernelConfig("", &specs.Spec{Root: &specs.Root{Path: rootfs}})

		if (errSpec == nil) != (errJSON == nil) {
			t.Fatalf("annotations: %v, urunc.json: %v", errSpec, errJSON)
		}
		if errSpec != nil {
			return
		}
		if !reflect.DeepEqual(fromSpec, fromJSON) {
			t.Fatalf("different config\n  annotations: %+v\n  urunc.json:  %+v", fromSpec, fromJSON)
		}
		if a, b := fromSpec.validateValues(), fromJSON.validateValues(); (a == nil) != (b == nil) {
			t.Fatalf("value checks disagree: %v vs %v", a, b)
		}
	})
}
