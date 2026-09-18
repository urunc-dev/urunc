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
	annotUruncPrefix     = "com.urunc.unikernel."
	annotType            = "com.urunc.unikernel.unikernelType"
	annotVersion         = "com.urunc.unikernel.unikernelVersion"
	annotBinary          = "com.urunc.unikernel.binary"
	annotHypervisor      = "com.urunc.unikernel.hypervisor"
	annotInitrd          = "com.urunc.unikernel.initrd"
	annotSnapshot        = "com.urunc.unikernel.snapshot"
	annotBlock           = "com.urunc.unikernel.block"
	annotBlockMntPoint   = "com.urunc.unikernel.blkMntPoint"
	annotMountRootfs     = "com.urunc.unikernel.mountRootfs"
	annotBootKernel      = "com.urunc.unikernel.bootKernel"
	annotBootInitrd      = "com.urunc.unikernel.bootInitrd"
	annotNetDev          = "com.urunc.unikernel.solo5NetDev"
	annotBlkDev          = "com.urunc.unikernel.solo5BlkDev"
	annotVAccel          = "com.urunc.unikernel.vAccel"
	annotRPCAddress      = "com.urunc.unikernel.RPCAddress"
	annotHyperlightPorts = "com.urunc.unikernel.hyperlightPorts"
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
	{hypervisors.QemuVmm, unikernels.FreeBSDUnikernel}:          true,
	{hypervisors.FirecrackerVmm, unikernels.LinuxUnikernel}:     true,
	{hypervisors.FirecrackerVmm, unikernels.UnikraftUnikernel}:  true,
	{hypervisors.FirecrackerVmm, unikernels.FreeBSDUnikernel}:   true,
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
	Snapshot         string `json:"com.urunc.unikernel.snapshot,omitempty"`
	Block            string `json:"com.urunc.unikernel.block,omitempty"`
	BlkMntPoint      string `json:"com.urunc.unikernel.blkMntPoint,omitempty"`
	MountRootfs      string `json:"com.urunc.unikernel.mountRootfs"`
	NetDev           string `json:"com.urunc.unikernel.solo5NetDev,omitempty"`
	BlkDev           string `json:"com.urunc.unikernel.solo5BlkDev,omitempty"`
	HyperlightPorts  string `json:"com.urunc.unikernel.hyperlightPorts,omitempty"`
	// The vAccel annotations are deliberately not part of urunc.json, since their
	// values are runtime specific and therefore we should only reach them
	// through the annotations of the spec.
	VAccel     string `json:"-"`
	RPCAddress string `json:"-"`
	// BootKernel and BootInitrd select a generic container boot: an unmodified
	// OCI image, which ships no unikernel of its own, is booted with a host
	// kernel and a host boot initrd. Both are absolute host paths and, like the
	// vAccel annotations, come only from the spec and never from urunc.json.
	BootKernel string `json:"-"`
	BootInitrd string `json:"-"`
}

// solo5DevNameRe constrains the valid values of a network or block device
// identifier for Solo5 guests when received from the annotations.
var solo5DevNameRe = regexp.MustCompile(`^[A-Za-z0-9]{1,67}$`)

// hyperlightPortsRe constrains the ports a hyperlight-unikraft guest may serve
// on to a comma-separated list of ports and LOW-HIGH ranges.
var hyperlightPortsRe = regexp.MustCompile(`^[0-9]{1,5}(-[0-9]{1,5})?(,[0-9]{1,5}(-[0-9]{1,5})?)*$`)

var ErrNotUnikernel = errors.New("this is not a unikernel container")

// validate checks if the mandatory configuration fields are present. It
// expects decoded values: the annotations of the spec as the shim hands them
// over, or a urunc.json that decode() has been through.
func (c *UnikernelConfig) validate() error {
	if c.UnikernelType == "" {
		return fmt.Errorf("unikernel configuration is missing mandatory field: %s", annotType)
	}
	if c.Hypervisor == "" {
		return fmt.Errorf("unikernel configuration is missing mandatory field: %s", annotHypervisor)
	}
	// The unikernel binary is what a monitor boots, so an image cannot do
	// without one. hyperlight-unikraft is the exception: hluk embeds a
	// kernel and takes the binary only as the kernel to boot instead. A
	// generic container boot takes its kernel from the bootKernel annotation.
	if c.UnikernelBinary == "" && hypervisors.VmmType(c.Hypervisor) != hypervisors.HyperlightVmm && !c.hasContainerBoot() {
		return fmt.Errorf("unikernel configuration is missing mandatory field: %s", annotBinary)
	}
	return nil
}

// hasContainerBoot reports whether the config asks for a generic container
// boot, i.e. at least one of the boot annotations is set. validateValues checks
// that both are set.
func (c *UnikernelConfig) hasContainerBoot() bool {
	return c.BootKernel != "" || c.BootInitrd != ""
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

	// urunc.json holds its values base64-encoded, and validate() looks at
	// the hypervisor's value, so decode first.
	err = jsonConf.decode()
	if err != nil {
		return nil, ErrNotUnikernel
	}

	err = jsonConf.validate()
	if err != nil {
		return nil, ErrNotUnikernel
	}

	// The vAccel annotations are runtime specific and deliberately never
	// stored in urunc.json. In any case, if they have been set at runtime
	// then they must be read from the spec annotations, so that images
	// configured through urunc.json can still use vAccel
	jsonConf.VAccel = spec.Annotations[annotVAccel]
	jsonConf.RPCAddress = spec.Annotations[annotRPCAddress]
	// The ports a hyperlight-unikraft guest may serve on are a choice of the
	// deployment first, and of the image second: an annotation of the spec
	// replaces whatever urunc.json says, and validateValues() checks it as it
	// does for an image configured through the spec.
	if ports := spec.Annotations[annotHyperlightPorts]; ports != "" {
		jsonConf.HyperlightPorts = ports
	}
	// The boot annotations are host paths and therefore runtime specific too.
	// Carry them so that validateValues can reject them for an image that also
	// ships its own unikernel through urunc.json.
	jsonConf.BootKernel = spec.Annotations[annotBootKernel]
	jsonConf.BootInitrd = spec.Annotations[annotBootInitrd]

	return jsonConf, nil
}

// getConfigFromSpec retrieves the urunc specific annotations from the spec and populates the Unikernel config.
func getConfigFromSpec(spec *specs.Spec) *UnikernelConfig {
	unikernelType := spec.Annotations[annotType]
	unikernelVersion := spec.Annotations[annotVersion]
	unikernelBinary := spec.Annotations[annotBinary]
	hypervisor := spec.Annotations[annotHypervisor]
	initrd := spec.Annotations[annotInitrd]
	snapshot := spec.Annotations[annotSnapshot]
	block := spec.Annotations[annotBlock]
	blkMntPoint := spec.Annotations[annotBlockMntPoint]
	MountRootfs := spec.Annotations[annotMountRootfs]
	bootKernel := spec.Annotations[annotBootKernel]
	bootInitrd := spec.Annotations[annotBootInitrd]
	netDev := spec.Annotations[annotNetDev]
	blkDev := spec.Annotations[annotBlkDev]
	hyperlightPorts := spec.Annotations[annotHyperlightPorts]
	vAccel := spec.Annotations[annotVAccel]
	rpcAddress := spec.Annotations[annotRPCAddress]

	// Generic container boot: the image ships no unikernel, so the host kernel
	// and boot initrd stand in for it. The rest of the contract is the same for
	// every such image (a Linux guest on qemu, booting from its own rootfs
	// shared into the guest), so default it here instead of requiring every
	// caller to spell it out. Explicit annotations still win and are validated
	// against that contract in validateValues. A single boot annotation counts
	// too, so that a half-specified boot is rejected there with a clear error
	// instead of falling through as a non-urunc container that then runs
	// natively.
	if bootKernel != "" || bootInitrd != "" {
		if unikernelType == "" {
			unikernelType = unikernels.LinuxUnikernel
		}
		if hypervisor == "" {
			hypervisor = string(hypervisors.QemuVmm)
		}
		if MountRootfs == "" {
			MountRootfs = "true"
		}
	}
	uniklog.WithFields(logrus.Fields{
		"unikernelType":    unikernelType,
		"unikernelVersion": unikernelVersion,
		"unikernelBinary":  unikernelBinary,
		"hypervisor":       hypervisor,
		"initrd":           initrd,
		"snapshot":         snapshot,
		"block":            block,
		"blkMntPoint":      blkMntPoint,
		"mountRootfs":      MountRootfs,
		"bootKernel":       bootKernel,
		"bootInitrd":       bootInitrd,
		"netDev":           netDev,
		"blkDev":           blkDev,
		"hyperlightPorts":  hyperlightPorts,
		"vAccel":           vAccel,
		"rpcAddress":       rpcAddress,
	}).WithField("source", "spec").Debug("urunc annotations")

	return &UnikernelConfig{
		UnikernelBinary:  unikernelBinary,
		UnikernelVersion: unikernelVersion,
		UnikernelType:    unikernelType,
		Hypervisor:       hypervisor,
		Initrd:           initrd,
		Snapshot:         snapshot,
		Block:            block,
		BlkMntPoint:      blkMntPoint,
		MountRootfs:      MountRootfs,
		BootKernel:       bootKernel,
		BootInitrd:       bootInitrd,
		NetDev:           netDev,
		BlkDev:           blkDev,
		HyperlightPorts:  hyperlightPorts,
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
		"snapshot":         tryDecode(conf.Snapshot),
		"block":            tryDecode(conf.Block),
		"blkMntPoint":      tryDecode(conf.BlkMntPoint),
		"mountRootfs":      tryDecode(conf.MountRootfs),
		"netDev":           tryDecode(conf.NetDev),
		"blkDev":           tryDecode(conf.BlkDev),
		"hyperlightPorts":  tryDecode(conf.HyperlightPorts),
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

	decoded, err = base64.StdEncoding.DecodeString(c.Snapshot)
	if err != nil {
		return fmt.Errorf("failed to decode Snapshot: %v", err)
	}
	c.Snapshot = string(decoded)

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

	decoded, err = base64.StdEncoding.DecodeString(c.HyperlightPorts)
	if err != nil {
		return fmt.Errorf("failed to decode hyperlightPorts: %v", err)
	}
	c.HyperlightPorts = string(decoded)

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
	if c.Snapshot != "" {
		myMap[annotSnapshot] = c.Snapshot
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
	if c.HyperlightPorts != "" {
		myMap[annotHyperlightPorts] = c.HyperlightPorts
	}
	if c.VAccel != "" {
		myMap[annotVAccel] = c.VAccel
	}
	if c.RPCAddress != "" {
		myMap[annotRPCAddress] = c.RPCAddress
	}
	if c.BootKernel != "" {
		myMap[annotBootKernel] = c.BootKernel
	}
	if c.BootInitrd != "" {
		myMap[annotBootInitrd] = c.BootInitrd
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
	// A snapshot is a saved hluk guest, kernel and rootfs included, so it
	// stands in for both the binary and the initrd, and hluk refuses either
	// alongside it. No other monitor can resume one.
	if c.Snapshot != "" {
		if monitor != hypervisors.HyperlightVmm {
			return fmt.Errorf("%s is only supported by %s", annotSnapshot, hypervisors.HyperlightVmm)
		}
		if c.UnikernelBinary != "" || c.Initrd != "" {
			return fmt.Errorf("%s cannot be combined with %s or %s", annotSnapshot, annotBinary, annotInitrd)
		}
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

	err = validateAnnotationPathClean(annotSnapshot, c.Snapshot, true)
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

	err = validateHyperlightPorts(monitor, c.HyperlightPorts)
	if err != nil {
		return err
	}

	err = c.validateContainerBoot()
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

// validateContainerBoot checks the generic container boot annotations. Both
// must be set together, each must be a clean absolute host path, and the rest
// of the configuration must describe the only supported shape of such a boot:
// a Linux guest on qemu that boots from the container rootfs shared into the
// guest, with no unikernel, initrd or block image coming from the image itself.
func (c *UnikernelConfig) validateContainerBoot() error {
	if !c.hasContainerBoot() {
		return nil
	}

	if c.BootKernel == "" || c.BootInitrd == "" {
		return fmt.Errorf("%s and %s must be set together", annotBootKernel, annotBootInitrd)
	}

	err := validateAnnotationHostPath(annotBootKernel, c.BootKernel)
	if err != nil {
		return err
	}

	err = validateAnnotationHostPath(annotBootInitrd, c.BootInitrd)
	if err != nil {
		return err
	}

	if c.UnikernelType != unikernels.LinuxUnikernel {
		return fmt.Errorf("%s requires %s to be %q, got %q", annotBootKernel, annotType, unikernels.LinuxUnikernel, c.UnikernelType)
	}

	// A generic container boot shares the image rootfs into the guest: over a
	// shared filesystem on qemu and cloud-hypervisor, or as a block device on
	// firecracker (which has no shared-fs) and, with the devmapper snapshotter,
	// on cloud-hypervisor. All boot the host kernel and initrd. Other monitors do
	// not support a Linux guest and are already rejected by the guest/monitor
	// pair check above.
	switch hypervisors.VmmType(c.Hypervisor) {
	case hypervisors.QemuVmm, hypervisors.FirecrackerVmm, hypervisors.CloudHypervisorVmm:
	default:
		return fmt.Errorf("%s requires %s to be one of %q, %q or %q, got %q", annotBootKernel, annotHypervisor,
			hypervisors.QemuVmm, hypervisors.FirecrackerVmm, hypervisors.CloudHypervisorVmm, c.Hypervisor)
	}

	if c.UnikernelBinary != "" {
		return fmt.Errorf("%s and %s are mutually exclusive: the kernel comes from the host", annotBootKernel, annotBinary)
	}

	if c.Initrd != "" {
		return fmt.Errorf("%s and %s are mutually exclusive: the initrd comes from the host", annotBootInitrd, annotInitrd)
	}

	if c.Block != "" {
		return fmt.Errorf("%s does not support a block image from %s", annotBootKernel, annotBlock)
	}

	// MountRootfs was already parsed above; a generic boot has no other rootfs
	// to offer the guest than the container's own.
	mountRootfs, _ := strconv.ParseBool(c.MountRootfs)
	if !mountRootfs {
		return fmt.Errorf("%s requires %s to be true", annotBootKernel, annotMountRootfs)
	}

	return nil
}

// validateAnnotationHostPath verifies that val is a clean, absolute path in the
// host, with the same plain character set as every other path annotation.
func validateAnnotationHostPath(key string, val string) error {
	err := validateAnnotationPathClean(key, val, true)
	if err != nil {
		return err
	}

	if !filepath.IsAbs(val) {
		return fmt.Errorf("%s must be an absolute host path, got %q", key, val)
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

// validateHyperlightPorts verifies that val lists the ports a
// hyperlight-unikraft guest may serve on: ports and LOW-HIGH ranges within
// 1-65535, separated by commas.
// An empty value is accepted, since the guest then serves on any port, as a
// container does. No other monitor can limit the ports of a guest.
func validateHyperlightPorts(monitor hypervisors.VmmType, val string) error {
	if val == "" {
		return nil
	}

	if monitor != hypervisors.HyperlightVmm {
		return fmt.Errorf("%s is only supported by %s", annotHyperlightPorts, hypervisors.HyperlightVmm)
	}

	if len(val) > maxAnnotationValueLen {
		return fmt.Errorf("%s value is longer than %d bytes", annotHyperlightPorts, maxAnnotationValueLen)
	}

	if !hyperlightPortsRe.MatchString(val) {
		return fmt.Errorf("%s must match %s, got %q", annotHyperlightPorts, hyperlightPortsRe.String(), val)
	}

	for _, item := range strings.Split(val, ",") {
		lowStr, highStr, isRange := strings.Cut(item, "-")
		if !isRange {
			highStr = lowStr
		}
		// The regexp already limited both to digits.
		low, _ := strconv.Atoi(lowStr)
		high, _ := strconv.Atoi(highStr)
		if low < 1 || high > 65535 || low > high {
			return fmt.Errorf("%s must list ports within 1-65535 and ranges from low to high, got %q", annotHyperlightPorts, item)
		}
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
