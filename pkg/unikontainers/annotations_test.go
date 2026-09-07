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
	"strings"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

func TestGetConfigFromSpec(t *testing.T) {
	t.Run("get config from spec success", func(t *testing.T) {
		t.Parallel()
		spec := &specs.Spec{
			Annotations: map[string]string{
				annotType:          "type1",
				annotBinary:        "binary1",
				annotHypervisor:    "hypervisor1",
				annotInitrd:        "initrd1",
				annotBlock:         "block1",
				annotBlockMntPoint: "point1",
				annotMountRootfs:   "true",
				annotNetDev:        "management",
				annotBlkDev:        "database",
			},
		}

		expectedConfig := &UnikernelConfig{
			UnikernelBinary: "binary1",
			UnikernelType:   "type1",
			Hypervisor:      "hypervisor1",
			Initrd:          "initrd1",
			Block:           "block1",
			BlkMntPoint:     "point1",
			MountRootfs:     "true",
			NetDev:          "management",
			BlkDev:          "database",
		}

		config := getConfigFromSpec(spec)
		assert.Equal(t, expectedConfig, config, "Expected config to match")
		err := config.validate()
		assert.NoError(t, err, "Expected a full config to be valid")
	})

	t.Run("get config from spec with empty annotations", func(t *testing.T) {
		t.Parallel()
		spec := &specs.Spec{
			Annotations: map[string]string{},
		}
		config := getConfigFromSpec(spec)
		assert.NotNil(t, config, "Expected config to be non-nil even with empty annotations")
		err := config.validate()
		assert.Error(t, err, "Expected validation to fail for an empty config")
		assert.ErrorContains(t, err, annotType, "Expected error to mention missing type field")
	})

	t.Run("get config from spec with partial (invalid) annotations", func(t *testing.T) {
		t.Parallel()
		spec := &specs.Spec{
			Annotations: map[string]string{
				annotType: "type1",
			},
		}

		expectedConfig := &UnikernelConfig{
			UnikernelType: "type1",
		}

		config := getConfigFromSpec(spec)
		assert.Equal(t, expectedConfig, config, "Expected partial config to match")
		err := config.validate()
		assert.Error(t, err, "Expected validation to fail for a partial config")
		assert.ErrorContains(t, err, annotHypervisor, "Expected error to mention missing hypervisor field")
	})
}

func TestGetConfigFromJSON(t *testing.T) {
	t.Run("get config from json success", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()

		// Create a valid urunc.json file
		expectedConfig := &UnikernelConfig{
			UnikernelBinary: "binary1",
			UnikernelType:   "type1",
			Hypervisor:      "hypervisor1",
			Initrd:          "initrd1",
			Block:           "block1",
			BlkMntPoint:     "point1",
			MountRootfs:     "true",
		}
		configData, err := json.Marshal(expectedConfig)
		assert.NoError(t, err)

		rootfsDir := filepath.Join(tempDir, rootfsDirName)
		err = os.Mkdir(rootfsDir, 0755)
		assert.NoError(t, err)

		configPath := filepath.Join(rootfsDir, uruncJSONFilename)
		err = os.WriteFile(configPath, configData, 0600)
		assert.NoError(t, err)

		// Call the function
		config, err := getConfigFromJSON(configPath)
		assert.NoError(t, err, "Expected no error in getting config from JSON")
		assert.Equal(t, expectedConfig, config, "Expected config to match")
	})

	t.Run("get config from json file not found", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, uruncJSONFilename)

		// Call the function with a missing urunc.json file
		_, err := getConfigFromJSON(configPath)
		assert.Error(t, err, "Expected an error for missing "+
			uruncJSONFilename+" file")
		assert.Contains(t, err.Error(), "no such file or directory", "Expected specific error message")
	})

	t.Run("get config from json is directory", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()

		// Create a directory instead of a urunc.json file
		rootfsDir := filepath.Join(tempDir, rootfsDirName)
		err := os.Mkdir(rootfsDir, 0755)
		assert.NoError(t, err)
		configDirPath := filepath.Join(rootfsDir, uruncJSONFilename)
		err = os.Mkdir(configDirPath, 0755)
		assert.NoError(t, err)

		// Call the function
		_, err = getConfigFromJSON(configDirPath)
		assert.Error(t, err, "Expected an error for "+uruncJSONFilename+" being a directory")
		assert.Contains(t, err.Error(), uruncJSONFilename+" is a directory", "Expected specific error message")
	})

	t.Run("get config from invalid JSON", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory
		tempDir := t.TempDir()

		// Create an invalid urunc.json file
		rootfsDir := filepath.Join(tempDir, rootfsDirName)
		err := os.Mkdir(rootfsDir, 0755)
		assert.NoError(t, err)

		configPath := filepath.Join(rootfsDir, uruncJSONFilename)
		err = os.WriteFile(configPath, []byte("invalid json"), 0600)
		assert.NoError(t, err)

		// Call the function
		_, err = getConfigFromJSON(configPath)
		assert.Error(t, err, "Expected an error for invalid "+uruncJSONFilename+" file")
		assert.Contains(t, err.Error(), "invalid character", "Expected specific error message")
	})
}

func TestDecode(t *testing.T) {
	t.Run("decode success", func(t *testing.T) {
		t.Parallel()
		// Prepare the encoded values
		encodedHypervisor := base64.StdEncoding.EncodeToString([]byte("testHypervisor"))
		encodedType := base64.StdEncoding.EncodeToString([]byte("testType"))
		encodedBinary := base64.StdEncoding.EncodeToString([]byte("testBinary"))
		encodedInitrd := base64.StdEncoding.EncodeToString([]byte("testInitrd"))

		config := &UnikernelConfig{
			Hypervisor:      encodedHypervisor,
			UnikernelType:   encodedType,
			UnikernelBinary: encodedBinary,
			Initrd:          encodedInitrd,
		}

		// Call the decode method
		err := config.decode()

		// Assert that no error occurred and the values are decoded correctly
		assert.NoError(t, err)
		assert.Equal(t, "testHypervisor", config.Hypervisor)
		assert.Equal(t, "testType", config.UnikernelType)
		assert.Equal(t, "testBinary", config.UnikernelBinary)
		assert.Equal(t, "testInitrd", config.Initrd)
	})

	t.Run("decode invalid base64", func(t *testing.T) {
		t.Parallel()
		// Prepare invalid base64 values
		invalidBase64 := "invalid-base64"

		config := &UnikernelConfig{
			Hypervisor:      invalidBase64,
			UnikernelType:   invalidBase64,
			UnikernelBinary: invalidBase64,
			Initrd:          invalidBase64,
		}
		// Call the decode method and expect an error
		err := config.decode()

		// Assert that an error occurred
		assert.Error(t, err)
	})
}

func TestMap(t *testing.T) {
	t.Run("unikernelConfig map success", func(t *testing.T) {
		t.Parallel()
		config := &UnikernelConfig{
			UnikernelBinary: "binary_value",
			UnikernelType:   "type_value",
			Hypervisor:      "hypervisor_value",
			Initrd:          "initrd_value",
			Block:           "block_value",
			BlkMntPoint:     "point_value",
			MountRootfs:     "false",
			NetDev:          "netdev_value",
			BlkDev:          "blkdev_value",
			VAccel:          "vsock",
			RPCAddress:      "vsock://2:1234",
		}
		expectedMap := map[string]string{
			annotType:          "type_value",
			annotHypervisor:    "hypervisor_value",
			annotBinary:        "binary_value",
			annotInitrd:        "initrd_value",
			annotBlock:         "block_value",
			annotBlockMntPoint: "point_value",
			annotMountRootfs:   "false",
			annotNetDev:        "netdev_value",
			annotBlkDev:        "blkdev_value",
			annotVAccel:        "vsock",
			annotRPCAddress:    "vsock://2:1234",
		}
		resultMap := config.Map()
		assert.Equal(t, expectedMap, resultMap)
	})
	t.Run("unikernelConfig map empty fields", func(t *testing.T) {
		t.Parallel()
		config := &UnikernelConfig{
			UnikernelBinary: "",
			UnikernelType:   "",
			Hypervisor:      "",
			Initrd:          "",
			Block:           "",
			BlkMntPoint:     "",
			MountRootfs:     "",
		}
		expectedMap := map[string]string{}
		resultMap := config.Map()
		assert.Equal(t, expectedMap, resultMap)
	})
	t.Run("unikernelConfig map partial fields", func(t *testing.T) {
		t.Parallel()
		config := &UnikernelConfig{
			UnikernelBinary: "binary_value",
			UnikernelType:   "",
			Hypervisor:      "",
			Initrd:          "initrd_value",
			Block:           "",
			BlkMntPoint:     "point_value",
			MountRootfs:     "0",
		}
		expectedMap := map[string]string{
			annotBinary:        "binary_value",
			annotInitrd:        "initrd_value",
			annotBlockMntPoint: "point_value",
			annotMountRootfs:   "0",
		}
		resultMap := config.Map()
		assert.Equal(t, expectedMap, resultMap)
	})

	t.Run("unikernelConfig map no fields", func(t *testing.T) {
		t.Parallel()
		config := &UnikernelConfig{}
		expectedMap := map[string]string{}
		resultMap := config.Map()
		assert.Equal(t, expectedMap, resultMap)
	})
}

// validAnnots returns the minimum set of annotations that validateValues
// accepts. The tests below override single entries to check a specific value.
func validAnnots() map[string]string {
	return map[string]string{
		annotType:       unikernels.UnikraftUnikernel,
		annotHypervisor: string(hypervisors.QemuVmm),
		annotBinary:     "/unikernel/app.unikraft",
	}
}

// validateAnnots runs validateValues over the given annotations, going
// through the same spec parsing that GetUnikernelConfig performs.
func validateAnnots(annots map[string]string) error {
	spec := &specs.Spec{Annotations: annots}

	return getConfigFromSpec(spec).validateValues()
}

// validateWith runs validateValues over the valid set, with key set to val.
func validateWith(key string, val string) error {
	annots := validAnnots()
	annots[key] = val

	return validateAnnots(annots)
}

func TestValidateValues(t *testing.T) {
	accepted := []struct {
		name string
		key  string
		val  string
	}{
		{"path with dots and dashes", annotBinary, "/unikernel/app-v2.0_x86_64.hvt"},
		{"relative path", annotBinary, "unikernel/app"},
		{"block image", annotBlock, "/data/block.img"},
		{"absolute mountpoint", annotBlockMntPoint, "/"},
		{"relative mountpoint", annotBlockMntPoint, "relative/path"},
		{"free form version", annotVersion, "not.a.version"},
		{"empty version", annotVersion, ""},
		{"solo5 device", annotNetDev, "management"},
		{"empty solo5 device", annotNetDev, ""},
		{"empty mountRootfs", annotMountRootfs, ""},
	}

	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateWith(tc.key, tc.val)
			assert.NoError(t, err, "Expected %s=%q to be accepted", tc.key, tc.val)
		})
	}

	rejected := []struct {
		name string
		key  string
		val  string
	}{
		{"non boolean mountRootfs", annotMountRootfs, "yes"},
		{"current directory binary", annotBinary, "."},
		{"current directory mountpoint", annotBlockMntPoint, "."},
		{"root binary", annotBinary, "/"},
		{"root initrd", annotInitrd, "/"},
		{"root block", annotBlock, "/"},
		{"path to parent", annotBinary, "../up"},
		{"path with a leading dash", annotBinary, "-rf"},
		{"unclean mountpoint", annotBlockMntPoint, "/mnt/../data"},
		{"path with a parent reference", annotInitrd, "/unikernel/../../data"},
		{"unclean path", annotInitrd, "/unikernel/./initrd"},
		{"path with a trailing slash", annotBlock, "/data/block.img/"},
		{"mountpoint with spaces", annotBlockMntPoint, "/mnt/my data"},
		{"multi word path", annotBinary, "hello there"},
		{"path with a semicolon", annotBinary, "/bin/a;b"},
		{"path with a backtick", annotBlock, "/x/`a`"},
		{"path with a comma", annotBlock, "/data/block.img,readonly=on"},
		{"path with an equals sign", annotBinary, "/unikernel/app=x"},
		{"path with a plus sign", annotBinary, "/unikernel/app+v2"},
		{"path with a colon", annotInitrd, "/unikernel/initrd:1"},
		{"solo5 device with whitespace", annotNetDev, "net 0"},
		{"solo5 device with a dash", annotNetDev, "net-0"},
		{"solo5 device with an underscore", annotBlkDev, "blk_0"},
	}

	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateWith(tc.key, tc.val)
			assert.Error(t, err, "Expected %s=%q to be rejected", tc.key, tc.val)
			assert.ErrorContains(t, err, tc.key, "Expected error to mention the annotation")
		})
	}

	t.Run("ignores the entries which are not urunc annotations", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		annots["urunc_config.monitors.qemu.default_vcpus"] = "2"
		annots["urunc_config.monitors.qemu.vhost"] = "yes"
		annots[annotCRICntrName] = "user container"
		annots["com.urunc.internal.rootfs.params"] = `{"Type":"block","Path":"/block.img"}`
		annots["com.urunc.unikernel.unknownAnnotation"] = "some value"

		err := validateAnnots(annots)
		assert.NoError(t, err, "Expected non urunc annotations to be ignored")
	})
}

// TestValidateValuesLength checks the length cap on the path and version values.
func TestValidateValuesLength(t *testing.T) {
	t.Run("accepts a path at the limit", func(t *testing.T) {
		t.Parallel()
		// A valid absolute path of exactly maxAnnotationValueLen bytes.
		val := "/" + strings.Repeat("a", maxAnnotationValueLen-1)
		err := validateWith(annotBinary, val)
		assert.NoError(t, err, "Expected a path at the length limit to be accepted")
	})

	t.Run("rejects an overlong path", func(t *testing.T) {
		t.Parallel()
		val := "/" + strings.Repeat("a", maxAnnotationValueLen)
		err := validateWith(annotBinary, val)
		assert.Error(t, err, "Expected an overlong path to be rejected")
		assert.ErrorContains(t, err, annotBinary, "Expected error to mention the annotation")
	})

	t.Run("rejects an overlong version", func(t *testing.T) {
		t.Parallel()
		val := strings.Repeat("1", maxAnnotationValueLen+1)
		err := validateWith(annotVersion, val)
		assert.Error(t, err, "Expected an overlong version to be rejected")
		assert.ErrorContains(t, err, annotVersion, "Expected error to mention the annotation")
	})

	t.Run("rejects an overlong rpc address", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		annots[annotVAccel] = "vsock"
		annots[annotRPCAddress] = "vsock://2:" + strings.Repeat("1", maxAnnotationValueLen)
		err := validateAnnots(annots)
		assert.Error(t, err, "Expected an overlong rpc address to be rejected")
		assert.ErrorContains(t, err, annotRPCAddress, "Expected error to mention the annotation")
	})
}

// TestValidateValuesGuestMonitorPairs checks that every declared guest and
// monitor combination is accepted, while unknown or mismatched ones are not.
func TestValidateValuesGuestMonitorPairs(t *testing.T) {
	for pair := range supportedGuestMonitorPairs {
		t.Run("accepts "+string(pair.monitor)+" with "+pair.guest, func(t *testing.T) {
			t.Parallel()
			annots := validAnnots()
			annots[annotHypervisor] = string(pair.monitor)
			annots[annotType] = pair.guest

			err := validateAnnots(annots)
			assert.NoError(t, err, "Expected a supported guest monitor pair to be accepted")
		})
	}

	rejected := []struct {
		name       string
		hypervisor string
		guest      string
	}{
		{"uppercase monitor", "QEMU", unikernels.UnikraftUnikernel},
		{"padded monitor", " qemu", unikernels.UnikraftUnikernel},
		{"unknown monitor", "bhyve", unikernels.UnikraftUnikernel},
		{"capitalized guest", string(hypervisors.QemuVmm), "Unikraft"},
		{"unknown guest", string(hypervisors.QemuVmm), "glueOS"},
		{"mirage on firecracker", string(hypervisors.FirecrackerVmm), unikernels.MirageUnikernel},
		{"rumprun on qemu", string(hypervisors.QemuVmm), unikernels.RumprunUnikernel},
		{"mewz on hvt", string(hypervisors.HvtVmm), unikernels.MewzUnikernel},
		{"hedge is not implemented", string(hypervisors.HedgeVmm), unikernels.UnikraftUnikernel},
	}

	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			annots := validAnnots()
			annots[annotHypervisor] = tc.hypervisor
			annots[annotType] = tc.guest

			err := validateAnnots(annots)
			assert.Error(t, err, "Expected %s with %s to be rejected", tc.hypervisor, tc.guest)
			assert.ErrorContains(t, err, tc.hypervisor, "Expected error to mention the monitor")
			assert.ErrorContains(t, err, tc.guest, "Expected error to mention the guest")
		})
	}
}

func TestValidateValuesVAccel(t *testing.T) {
	// vAccelAnnots returns a valid set for the given monitor, with the vAccel
	// annotations set to the given values.
	vAccelAnnots := func(monitor hypervisors.VmmType, vAccel string, address string) map[string]string {
		annots := validAnnots()
		annots[annotHypervisor] = string(monitor)
		annots[annotType] = unikernels.LinuxUnikernel
		if vAccel != "" {
			annots[annotVAccel] = vAccel
		}
		if address != "" {
			annots[annotRPCAddress] = address
		}

		return annots
	}

	accepted := []struct {
		name    string
		monitor hypervisors.VmmType
		address string
	}{
		{"qemu vsock address", hypervisors.QemuVmm, "vsock://2:1234"},
		{"qemu lowest port", hypervisors.QemuVmm, "vsock://2:1"},
		{"qemu highest port", hypervisors.QemuVmm, "vsock://2:65535"},
		{"firecracker unix address", hypervisors.FirecrackerVmm, "unix:///tmp/vaccel.sock_1234"},
		{"firecracker nested directory", hypervisors.FirecrackerVmm, "unix:///var/run/urunc/vaccel.sock_5678"},
		{"firecracker directory with a dash", hypervisors.FirecrackerVmm, "unix:///run/my-vaccel_dir/vaccel.sock_1"},
	}

	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateAnnots(vAccelAnnots(tc.monitor, "vsock", tc.address))
			assert.NoError(t, err, "Expected %s to be accepted for %s", tc.address, tc.monitor)
		})
	}

	rejected := []struct {
		name    string
		monitor hypervisors.VmmType
		vAccel  string
		address string
	}{
		{"unknown vAccel type", hypervisors.QemuVmm, "vSock", "vsock://2:1234"},
		{"empty vAccel type", hypervisors.QemuVmm, " ", "vsock://2:1234"},
		{"missing address", hypervisors.QemuVmm, "vsock", ""},
		{"monitor without vaccel support", hypervisors.CloudHypervisorVmm, "vsock", "vsock://2:1234"},
		{"wrong cid", hypervisors.QemuVmm, "vsock", "vsock://3:1234"},
		{"port zero", hypervisors.QemuVmm, "vsock", "vsock://2:0"},
		{"port out of range", hypervisors.QemuVmm, "vsock", "vsock://2:65536"},
		{"port with leading zero", hypervisors.QemuVmm, "vsock", "vsock://2:01234"},
		{"overlong port", hypervisors.QemuVmm, "vsock", "vsock://2:99999999999"},
		{"missing port", hypervisors.QemuVmm, "vsock", "vsock://2:"},
		{"garbage address", hypervisors.QemuVmm, "vsock", "vsock://invalid"},
		{"firecracker address on qemu", hypervisors.QemuVmm, "vsock", "unix:///tmp/vaccel.sock_1234"},
		{"qemu address on firecracker", hypervisors.FirecrackerVmm, "vsock", "vsock://2:1234"},
		{"wrong socket name", hypervisors.FirecrackerVmm, "vsock", "unix:///tmp/test.sock"},
		{"relative socket directory", hypervisors.FirecrackerVmm, "vsock", "unix://tmp/vaccel.sock_1"},
		{"empty socket directory", hypervisors.FirecrackerVmm, "vsock", "unix:///vaccel.sock_1"},
		{"socket directory with a parent reference", hypervisors.FirecrackerVmm, "vsock", "unix:///tmp/../other/vaccel.sock_1"},
		{"socket directory with a dot element", hypervisors.FirecrackerVmm, "vsock", "unix:///tmp/./vaccel.sock_1"},
		{"socket directory with whitespace", hypervisors.FirecrackerVmm, "vsock", "unix:///tmp/my dir/vaccel.sock_1"},
		{"socket directory with a semicolon", hypervisors.FirecrackerVmm, "vsock", "unix:///tmp/a;b/vaccel.sock_1"},
		{"socket directory with a double slash", hypervisors.FirecrackerVmm, "vsock", "unix:///tmp//vaccel.sock_1"},
	}

	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateAnnots(vAccelAnnots(tc.monitor, tc.vAccel, tc.address))
			assert.Error(t, err, "Expected %s to be rejected for %s", tc.address, tc.monitor)
		})
	}

	t.Run("accepts an absent vAccel", func(t *testing.T) {
		t.Parallel()
		err := validateAnnots(vAccelAnnots(hypervisors.QemuVmm, "", ""))
		assert.NoError(t, err, "Expected no error without the vAccel annotations")
	})

	t.Run("rejects an address without vAccel", func(t *testing.T) {
		t.Parallel()
		err := validateAnnots(vAccelAnnots(hypervisors.QemuVmm, "", "vsock://2:1234"))
		assert.Error(t, err, "Expected an rpc address without vAccel to be rejected")
		assert.ErrorContains(t, err, annotVAccel, "Expected error to mention the annotation")
	})

	// vAccel is only supported through the annotations of the spec. A urunc.json
	// must not be able to enable it.
	t.Run("ignores the vAccel entries of urunc.json", func(t *testing.T) {
		t.Parallel()
		jsonConf := map[string]string{
			annotType:       base64.StdEncoding.EncodeToString([]byte(unikernels.UnikraftUnikernel)),
			annotHypervisor: base64.StdEncoding.EncodeToString([]byte(hypervisors.QemuVmm)),
			annotBinary:     base64.StdEncoding.EncodeToString([]byte("/unikernel/app")),
			annotVAccel:     base64.StdEncoding.EncodeToString([]byte("vsock")),
			annotRPCAddress: base64.StdEncoding.EncodeToString([]byte("vsock://2:1234")),
		}
		configData, err := json.Marshal(jsonConf)
		assert.NoError(t, err)

		rootfsDir := filepath.Join(t.TempDir(), rootfsDirName)
		err = os.Mkdir(rootfsDir, 0755)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(rootfsDir, uruncJSONFilename), configData, 0600)
		assert.NoError(t, err)

		conf, err := getConfigFromJSON(filepath.Join(rootfsDir, uruncJSONFilename))
		assert.NoError(t, err)
		assert.Empty(t, conf.VAccel, "Expected urunc.json not to set vAccel")
		assert.Empty(t, conf.RPCAddress, "Expected urunc.json not to set the rpc address")
	})
}

// GetUnikernelConfig must still read the vAccel annotations from the spec when
// the rest of the config comes from urunc.json. Otherwise a runtime supplied
// --annotation com.urunc.unikernel.vAccel would be silently lost for images
// configured through urunc.json.
func TestGetUnikernelConfigVAccelFromJSONFallback(t *testing.T) {
	t.Parallel()

	jsonConf := map[string]string{
		annotType:       base64.StdEncoding.EncodeToString([]byte(unikernels.LinuxUnikernel)),
		annotHypervisor: base64.StdEncoding.EncodeToString([]byte(hypervisors.QemuVmm)),
		annotBinary:     base64.StdEncoding.EncodeToString([]byte("/unikernel/app")),
	}
	configData, err := json.Marshal(jsonConf)
	assert.NoError(t, err)

	bundle := t.TempDir()
	rootfsDir := filepath.Join(bundle, rootfsDirName)
	err = os.Mkdir(rootfsDir, 0755)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(rootfsDir, uruncJSONFilename), configData, 0600)
	assert.NoError(t, err)

	// The spec lacks the mandatory urunc annotations, so GetUnikernelConfig
	// falls back to urunc.json. The runtime still supplies vAccel through the
	// spec annotations though, and those must reach the resolved config.
	spec := &specs.Spec{
		Root: &specs.Root{Path: rootfsDirName},
		Annotations: map[string]string{
			annotVAccel:     "vsock",
			annotRPCAddress: "vsock://2:1234",
		},
	}

	conf, err := GetUnikernelConfig(bundle, spec)
	assert.NoError(t, err)
	assert.Equal(t, "vsock", conf.VAccel, "Expected vAccel to be read from the spec")
	assert.Equal(t, "vsock://2:1234", conf.RPCAddress, "Expected the rpc address to be read from the spec")
}

// writeBundle creates a bundle directory with a config.json holding annots.
func writeBundle(t *testing.T, annots map[string]string) string {
	t.Helper()

	bundle := t.TempDir()
	spec := specs.Spec{
		Version:     specs.Version,
		Linux:       &specs.Linux{},
		Root:        &specs.Root{Path: rootfsDirName},
		Annotations: annots,
	}

	data, err := json.Marshal(spec)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(bundle, configFilename), data, 0600)
	assert.NoError(t, err)

	return bundle
}
