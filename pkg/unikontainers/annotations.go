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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

// Important: Unfortunately Golang does not allow to use constant values for
// struct tags. As a result, please always keep the constant definitions and the
// UnikernelConfig struct below in sync.

// Urunc specific annotations
// Always keep it in sync with the struct UnikernelConfig struct
const (
	annotUruncPrefix   = "com.urunc.unikernel."
	annotType          = "com.urunc.unikernel.unikernelType"
	annotVersion       = "com.urunc.unikernel.unikernelVersion"
	annotBinary        = "com.urunc.unikernel.binary"
	annotHypervisor    = "com.urunc.unikernel.hypervisor"
	annotInitrd        = "com.urunc.unikernel.initrd"
	annotBlock         = "com.urunc.unikernel.block"
	annotBlockMntPoint = "com.urunc.unikernel.blkMntPoint"
	annotMountRootfs   = "com.urunc.unikernel.mountRootfs"
	annotBootKernel    = "com.urunc.unikernel.bootKernel"
	annotBootInitrd    = "com.urunc.unikernel.bootInitrd"
	annotNetDev        = "com.urunc.unikernel.solo5NetDev"
	annotBlkDev        = "com.urunc.unikernel.solo5BlkDev"
	annotVAccel        = "com.urunc.unikernel.vAccel"
	annotRPCAddress    = "com.urunc.unikernel.RPCAddress"
)

// Annotations that other runtimes set and urunc only reads.
const (
	annotCRICntrName  = "io.kubernetes.cri.container-name"
	criQueueProxyCntr = "queue-proxy"
	criUserCntr       = "user-container"
)

// allowedPathRe constrains the characters of the filepaths that we receive
// through the annotations, since these paths end up in the command line of the
// monitor. Instead of listing every rune that is unsafe, we accept only the ones
// that a filepath inside an image is expected to have.
var allowedPathRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

type guestMonitorPair struct {
	monitor hypervisors.VmmType
	guest   string
}

var supportedGuestMonitorPairs = map[guestMonitorPair]bool{
	{hypervisors.HvtVmm, unikernels.RumprunUnikernel}:           true,
	{hypervisors.HvtVmm, unikernels.MirageUnikernel}:            true,
	{hypervisors.SptVmm, unikernels.RumprunUnikernel}:           true,
	{hypervisors.SptVmm, unikernels.MirageUnikernel}:            true,
	{hypervisors.QemuVmm, unikernels.MewzUnikernel}:             true,
	{hypervisors.QemuVmm, unikernels.MirageUnikernel}:           true,
	{hypervisors.QemuVmm, unikernels.UnikraftUnikernel}:         true,
	{hypervisors.QemuVmm, unikernels.LinuxUnikernel}:            true,
	{hypervisors.QemuVmm, unikernels.HermitUnikernel}:           true,
	{hypervisors.FirecrackerVmm, unikernels.LinuxUnikernel}:     true,
	{hypervisors.FirecrackerVmm, unikernels.UnikraftUnikernel}:  true,
	{hypervisors.CloudHypervisorVmm, unikernels.LinuxUnikernel}: true,
	{hypervisors.HyperlightVmm, unikernels.UnikraftUnikernel}:   true,
}

// A UnikernelConfig struct holds the info provided by bima image on how to execute our unikernel
type UnikernelConfig struct {
	UnikernelType    string `json:"com.urunc.unikernel.unikernelType"`
	UnikernelVersion string `json:"com.urunc.unikernel.unikernelVersion"`
	UnikernelBinary  string `json:"com.urunc.unikernel.binary"`
	Hypervisor       string `json:"com.urunc.unikernel.hypervisor"`
	Initrd           string `json:"com.urunc.unikernel.initrd,omitempty"`
	Block            string `json:"com.urunc.unikernel.block,omitempty"`
	BlkMntPoint      string `json:"com.urunc.unikernel.blkMntPoint,omitempty"`
	MountRootfs      string `json:"com.urunc.unikernel.mountRootfs"`
	NetDev           string `json:"com.urunc.unikernel.solo5NetDev,omitempty"`
	BlkDev           string `json:"com.urunc.unikernel.solo5BlkDev,omitempty"`
	// The vAccel annotations are deliberately not part of urunc.json, since their
	// values are runtime specific and therefore we should only reach them
	// through the annotations of the spec.
	VAccel     string `json:"-"`
	RPCAddress string `json:"-"`
}

// solo5DevNameRe constrains the valid values of a network or block device
// identifier for Solo5 guests when received from the annotations.
var solo5DevNameRe = regexp.MustCompile(`^[A-Za-z0-9]{1,67}$`)

var ErrNotUnikernel = errors.New("this is not a unikernel container")

// validate checks if the mandatory configuration fields are present.
func (c *UnikernelConfig) validate() error {
	if c.UnikernelType == "" {
		return fmt.Errorf("unikernel configuration is missing mandatory field: %s", annotType)
	}
	if c.Hypervisor == "" {
		return fmt.Errorf("unikernel configuration is missing mandatory field: %s", annotHypervisor)
	}
	if c.UnikernelBinary == "" {
		return fmt.Errorf("unikernel configuration is missing mandatory field: %s", annotBinary)
	}
	return nil
}

// GetUnikernelConfig tries to get the Unikernel config from the bundle annotations.
// If that fails, it gets the Unikernel config from the urunc.json file inside the rootfs.
func GetUnikernelConfig(bundleDir string, spec *specs.Spec) (*UnikernelConfig, error) {
	conf := getConfigFromSpec(spec)
	err := conf.validate()
	if err == nil {
		// TODO: in case of urunc executed without shim, the annotations would remain encoded
		return conf, nil
	}

	// Failed to fetch urunc annotations from spec, fallback to urunc.json
	uniklog.Info("failed to fetch urunc annotations from spec, fallback to urunc.json")
	rootFSDir := spec.Root.Path
	if !filepath.IsAbs(rootFSDir) {
		rootFSDir = filepath.Join(bundleDir, rootFSDir)
	}
	jsonFilePath, err := securejoin.SecureJoin(rootFSDir, uruncJSONFilename)
	if err != nil {
		return nil, err
	}

	jsonConf, err := getConfigFromJSON(jsonFilePath)
	if err != nil {
		return nil, ErrNotUnikernel
	}

	err = jsonConf.validate()
	if err != nil {
		return nil, ErrNotUnikernel
	}

	err = jsonConf.decode()
	if err != nil {
		return nil, ErrNotUnikernel
	}

	// The vAccel annotations are runtime specific and deliberately never
	// stored in urunc.json. In any case, if they have been set at runtime
	// then they must be read from the spec annotations, so that images
	// configured through urunc.json can still use vAccel
	jsonConf.VAccel = spec.Annotations[annotVAccel]
	jsonConf.RPCAddress = spec.Annotations[annotRPCAddress]

	return jsonConf, nil
}

// getConfigFromSpec retrieves the urunc specific annotations from the spec and populates the Unikernel config.
func getConfigFromSpec(spec *specs.Spec) *UnikernelConfig {
	unikernelType := spec.Annotations[annotType]
	unikernelVersion := spec.Annotations[annotVersion]
	unikernelBinary := spec.Annotations[annotBinary]
	hypervisor := spec.Annotations[annotHypervisor]
	initrd := spec.Annotations[annotInitrd]
	block := spec.Annotations[annotBlock]
	blkMntPoint := spec.Annotations[annotBlockMntPoint]
	MountRootfs := spec.Annotations[annotMountRootfs]
	netDev := spec.Annotations[annotNetDev]
	blkDev := spec.Annotations[annotBlkDev]
	vAccel := spec.Annotations[annotVAccel]
	rpcAddress := spec.Annotations[annotRPCAddress]
	uniklog.WithFields(logrus.Fields{
		"unikernelType":    unikernelType,
		"unikernelVersion": unikernelVersion,
		"unikernelBinary":  unikernelBinary,
		"hypervisor":       hypervisor,
		"initrd":           initrd,
		"block":            block,
		"blkMntPoint":      blkMntPoint,
		"mountRootfs":      MountRootfs,
		"netDev":           netDev,
		"blkDev":           blkDev,
		"vAccel":           vAccel,
		"rpcAddress":       rpcAddress,
	}).WithField("source", "spec").Debug("urunc annotations")

	return &UnikernelConfig{
		UnikernelBinary:  unikernelBinary,
		UnikernelVersion: unikernelVersion,
		UnikernelType:    unikernelType,
		Hypervisor:       hypervisor,
		Initrd:           initrd,
		Block:            block,
		BlkMntPoint:      blkMntPoint,
		MountRootfs:      MountRootfs,
		NetDev:           netDev,
		BlkDev:           blkDev,
		VAccel:           vAccel,
		RPCAddress:       rpcAddress,
	}
}

// getConfigFromJSON retrieves the Unikernel config parameters from the urunc.json file inside the rootfs.
func getConfigFromJSON(jsonFilePath string) (*UnikernelConfig, error) {
	file, err := os.Open(jsonFilePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if fileInfo.IsDir() {
		return nil, errors.New(uruncJSONFilename + " is a directory")
	}

	byteData, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var conf UnikernelConfig
	err = json.Unmarshal(byteData, &conf)
	if err != nil {
		return nil, err
	}
	uniklog.WithFields(logrus.Fields{
		"unikernelType":    tryDecode(conf.UnikernelType),
		"unikernelVersion": tryDecode(conf.UnikernelVersion),
		"unikernelBinary":  tryDecode(conf.UnikernelBinary),
		"hypervisor":       tryDecode(conf.Hypervisor),
		"initrd":           tryDecode(conf.Initrd),
		"block":            tryDecode(conf.Block),
		"blkMntPoint":      tryDecode(conf.BlkMntPoint),
		"mountRootfs":      tryDecode(conf.MountRootfs),
		"netDev":           tryDecode(conf.NetDev),
		"blkDev":           tryDecode(conf.BlkDev),
	}).WithField("source", uruncJSONFilename).Debug("urunc annotations")

	return &conf, nil
}

func tryDecode(s string) string {
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		uniklog.WithError(err).Errorf("Failed to decode string: %s", s)
		return s
	}
	return string(decoded)
}

// decode decodes the base64 encoded values of the Unikernel config
func (c *UnikernelConfig) decode() error {
	decoded, err := base64.StdEncoding.DecodeString(c.Hypervisor)
	if err != nil {
		return fmt.Errorf("failed to decode Hypervisor: %v", err)
	}
	c.Hypervisor = string(decoded)

	decoded, err = base64.StdEncoding.DecodeString(c.UnikernelType)
	if err != nil {
		return fmt.Errorf("failed to decode UnikernelType: %v", err)
	}
	c.UnikernelType = string(decoded)

	decoded, err = base64.StdEncoding.DecodeString(c.UnikernelVersion)
	if err != nil {
		return fmt.Errorf("failed to decode UnikernelVersion: %v", err)
	}
	c.UnikernelVersion = string(decoded)

	decoded, err = base64.StdEncoding.DecodeString(c.UnikernelBinary)
	if err != nil {
		return fmt.Errorf("failed to decode UnikernelBinary: %v", err)
	}
	c.UnikernelBinary = string(decoded)

	decoded, err = base64.StdEncoding.DecodeString(c.Initrd)
	if err != nil {
		return fmt.Errorf("failed to decode Initrd: %v", err)
	}
	c.Initrd = string(decoded)

	decoded, err = base64.StdEncoding.DecodeString(c.Block)
	if err != nil {
		return fmt.Errorf("failed to decode Block: %v", err)
	}
	c.Block = string(decoded)

	decoded, err = base64.StdEncoding.DecodeString(c.BlkMntPoint)
	if err != nil {
		return fmt.Errorf("failed to decode BlockMntPoint: %v", err)
	}
	c.BlkMntPoint = string(decoded)

	decoded, err = base64.StdEncoding.DecodeString(c.MountRootfs)
	if err != nil {
		return fmt.Errorf("failed to decode mountRootfs: %v", err)
	}
	c.MountRootfs = string(decoded)

	decoded, err = base64.StdEncoding.DecodeString(c.NetDev)
	if err != nil {
		return fmt.Errorf("failed to decode netDev: %v", err)
	}
	c.NetDev = string(decoded)
	decoded, err = base64.StdEncoding.DecodeString(c.BlkDev)
	if err != nil {
		return fmt.Errorf("failed to decode blkDev: %v", err)
	}
	c.BlkDev = string(decoded)

	return nil
}

// Map returns a map containing the Unikernel config data
func (c *UnikernelConfig) Map() map[string]string {
	myMap := make(map[string]string)
	if c.UnikernelType != "" {
		myMap[annotType] = c.UnikernelType
	}
	if c.UnikernelVersion != "" {
		myMap[annotVersion] = c.UnikernelVersion
	}
	if c.Hypervisor != "" {
		myMap[annotHypervisor] = c.Hypervisor
	}
	if c.UnikernelBinary != "" {
		myMap[annotBinary] = c.UnikernelBinary
	}
	if c.Initrd != "" {
		myMap[annotInitrd] = c.Initrd
	}
	if c.Block != "" {
		myMap[annotBlock] = c.Block
	}
	if c.BlkMntPoint != "" {
		myMap[annotBlockMntPoint] = c.BlkMntPoint
	}
	if c.MountRootfs != "" {
		myMap[annotMountRootfs] = c.MountRootfs
	}
	if c.NetDev != "" {
		myMap[annotNetDev] = c.NetDev
	}
	if c.BlkDev != "" {
		myMap[annotBlkDev] = c.BlkDev
	}
	if c.VAccel != "" {
		myMap[annotVAccel] = c.VAccel
	}
	if c.RPCAddress != "" {
		myMap[annotRPCAddress] = c.RPCAddress
	}

	return myMap
}

// maxAnnotationValueLen bounds the length of the unbounded annotation values.
// Choosing 4096 since it limits paths and version values.
const maxAnnotationValueLen = 4096

// validateValues checks that the value of every urunc annotation has the
// expected format. It complements validate(), which only checks that the
// mandatory annotations are present.
func (c *UnikernelConfig) validateValues() error {
	monitor := hypervisors.VmmType(c.Hypervisor)
	if !supportedGuestMonitorPairs[guestMonitorPair{monitor, c.UnikernelType}] {
		return fmt.Errorf("unsupported guest monitor pair %s %s", c.Hypervisor, c.UnikernelType)
	}

	if len(c.UnikernelVersion) > maxAnnotationValueLen {
		return fmt.Errorf("%s value is longer than %d bytes", annotVersion, maxAnnotationValueLen)
	}

	err := validateAnnotationPathClean(annotBinary, c.UnikernelBinary, true)
	if err != nil {
		return err
	}

	err = validateAnnotationPathClean(annotInitrd, c.Initrd, true)
	if err != nil {
		return err
	}

	err = validateAnnotationPathClean(annotBlock, c.Block, true)
	if err != nil {
		return err
	}

	err = validateAnnotationPathClean(annotBlockMntPoint, c.BlkMntPoint, false)
	if err != nil {
		return err
	}

	if c.MountRootfs != "" {
		_, err = strconv.ParseBool(c.MountRootfs)
		if err != nil {
			return fmt.Errorf("invalid value %q for %s: expected a boolean: %w", c.MountRootfs, annotMountRootfs, err)
		}
	}

	err = validateSolo5DevName(annotNetDev, c.NetDev)
	if err != nil {
		return err
	}

	err = validateSolo5DevName(annotBlkDev, c.BlkDev)
	if err != nil {
		return err
	}

	if c.VAccel == "" {
		if c.RPCAddress != "" {
			return fmt.Errorf("%s is set, but %s is not", annotRPCAddress, annotVAccel)
		}

		return nil
	}

	if c.VAccel != "vsock" {
		return fmt.Errorf("invalid value %q for %s: expected \"vsock\"", c.VAccel, annotVAccel)
	}

	if c.RPCAddress == "" {
		return fmt.Errorf("vAccel is set but %s is missing", annotRPCAddress)
	}

	if len(c.RPCAddress) > maxAnnotationValueLen {
		return fmt.Errorf("%s value is longer than %d bytes", annotRPCAddress, maxAnnotationValueLen)
	}

	regex, exists := vAccelAddressRe[c.Hypervisor]
	if !exists {
		return fmt.Errorf("%s does not support vAccel", c.Hypervisor)
	}

	if !regex.MatchString(c.RPCAddress) {
		return fmt.Errorf("invalid value %q for %s: it does not match the expected format for %s", c.RPCAddress, annotRPCAddress, c.Hypervisor)
	}

	return nil
}

// validateSolo5DevName verifies that val is a valid Solo5 network or block
// device identifier. An empty value is accepted, since the device is optional.
func validateSolo5DevName(key string, val string) error {
	if val == "" {
		return nil
	}

	if !solo5DevNameRe.MatchString(val) {
		return fmt.Errorf("%s must match %s, got %q", key, solo5DevNameRe.String(), val)
	}

	return nil
}

// validateAnnotationPathClean verifies that val looks like a filepath with
// simple plain characters and is also clean.
func validateAnnotationPathClean(key string, val string, rejectRoot bool) error {
	if val == "" {
		return nil
	}

	if len(val) > maxAnnotationValueLen {
		return fmt.Errorf("%s value is longer than %d bytes", key, maxAnnotationValueLen)
	}

	if !allowedPathRe.MatchString(val) {
		return fmt.Errorf("%s must match %s, got %q", key, allowedPathRe.String(), val)
	}

	if strings.HasPrefix(val, "..") {
		return fmt.Errorf("%s must not start with '..', got %q", key, val)
	}

	if strings.HasPrefix(val, "-") {
		return fmt.Errorf("%s must not start with '-', got %q", key, val)
	}

	cleaned := filepath.Clean(val)
	if cleaned != val {
		return fmt.Errorf("%s must be a clean path, got %q", key, val)
	}

	if cleaned == "." {
		return fmt.Errorf("%s must not be '.', got %q", key, val)
	}

	if cleaned == "/" && rejectRoot {
		return fmt.Errorf("%s must not be '/', got %q", key, val)
	}

	return nil
}
