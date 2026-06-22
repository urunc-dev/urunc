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
	"strings"
	"unicode/utf8"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

var ErrEmptyAnnotations = errors.New("spec annotations are empty")

// Important: Unfortunately GOlang does not allow to use constant values for
// struct tagsAs a result, please always keep the constant definitions and the
// UnikernelConfig struct below in sync.

// Urunc specific annotations
// ALways keep it in sync with the struct UnikernelConfig struct
const (
	annotType          = "com.urunc.unikernel.unikernelType"
	annotVersion       = "com.urunc.unikernel.unikernelVersion"
	annotBinary        = "com.urunc.unikernel.binary"
	annotCmdLine       = "com.urunc.unikernel.cmdline"
	annotHypervisor    = "com.urunc.unikernel.hypervisor"
	annotInitrd        = "com.urunc.unikernel.initrd"
	annotBlock         = "com.urunc.unikernel.block"
	annotBlockMntPoint = "com.urunc.unikernel.blkMntPoint"
	annotMountRootfs   = "com.urunc.unikernel.mountRootfs"
)

// A UnikernelConfig struct holds the info provided by bima image on how to execute our unikernel
type UnikernelConfig struct {
	UnikernelType    string `json:"com.urunc.unikernel.unikernelType"`
	UnikernelVersion string `json:"com.urunc.unikernel.unikernelVersion"`
	UnikernelCmd     string `json:"com.urunc.unikernel.cmdline,omitempty"`
	UnikernelBinary  string `json:"com.urunc.unikernel.binary"`
	Hypervisor       string `json:"com.urunc.unikernel.hypervisor"`
	Initrd           string `json:"com.urunc.unikernel.initrd,omitempty"`
	Block            string `json:"com.urunc.unikernel.block,omitempty"`
	BlkMntPoint      string `json:"com.urunc.unikernel.blkMntPoint,omitempty"`
	MountRootfs      string `json:"com.urunc.unikernel.mountRootfs"`
}

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
	if err := conf.validate(); err == nil {
		// TODO: in case of urunc executed without shim, the annotations would remain encoded
		return conf, nil
	}

	// Failed to fetch urunc annotations from spec, fallback to urunc.json
	uniklog.Info("failed to fetch urunc annotations from spec, fallback to urunc.json")
	rootFSDir := spec.Root.Path
	var jsonFilePath string
	if filepath.IsAbs(rootFSDir) {
		jsonFilePath = filepath.Join(rootFSDir, uruncJSONFilename)
	} else {
		jsonFilePath = filepath.Join(bundleDir, rootFSDir, uruncJSONFilename)
	}

	jsonConf, err := getConfigFromJSON(jsonFilePath)
	if err != nil {
		return nil, fmt.Errorf("config not found in spec annotations or in %s: %w", uruncJSONFilename, err)
	}

	if err := jsonConf.validate(); err != nil {
		return nil, fmt.Errorf("invalid unikernel config from %s: %w", uruncJSONFilename, err)
	}

	if err := jsonConf.decode(); err != nil {
		return nil, err
	}
	return jsonConf, nil
}

// getConfigFromSpec retrieves the urunc specific annotations from the spec and populates the Unikernel config.
func getConfigFromSpec(spec *specs.Spec) *UnikernelConfig {
	unikernelType := spec.Annotations[annotType]
	unikernelVersion := spec.Annotations[annotVersion]
	unikernelCmd := spec.Annotations[annotCmdLine]
	unikernelBinary := spec.Annotations[annotBinary]
	hypervisor := spec.Annotations[annotHypervisor]
	initrd := spec.Annotations[annotInitrd]
	block := spec.Annotations[annotBlock]
	blkMntPoint := spec.Annotations[annotBlockMntPoint]
	MountRootfs := spec.Annotations[annotMountRootfs]
	uniklog.WithFields(logrus.Fields{
		"unikernelType":    logConfigValue(unikernelType),
		"unikernelVersion": logConfigValue(unikernelVersion),
		"unikernelCmd":     logConfigValue(unikernelCmd),
		"unikernelBinary":  logConfigValue(unikernelBinary),
		"hypervisor":       logConfigValue(hypervisor),
		"initrd":           logConfigValue(initrd),
		"block":            logConfigValue(block),
		"blkMntPoint":      logConfigValue(blkMntPoint),
		"mountRootfs":      logConfigValue(MountRootfs),
	}).WithField("source", "spec").Debug("urunc annotations")

	return &UnikernelConfig{
		UnikernelBinary:  unikernelBinary,
		UnikernelVersion: unikernelVersion,
		UnikernelType:    unikernelType,
		UnikernelCmd:     unikernelCmd,
		Hypervisor:       hypervisor,
		Initrd:           initrd,
		Block:            block,
		BlkMntPoint:      blkMntPoint,
		MountRootfs:      MountRootfs,
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
		"unikernelType":    logConfigValue(conf.UnikernelType),
		"unikernelVersion": logConfigValue(conf.UnikernelVersion),
		"unikernelCmd":     logConfigValue(conf.UnikernelCmd),
		"unikernelBinary":  logConfigValue(conf.UnikernelBinary),
		"hypervisor":       logConfigValue(conf.Hypervisor),
		"initrd":           logConfigValue(conf.Initrd),
		"block":            logConfigValue(conf.Block),
		"blkMntPoint":      logConfigValue(conf.BlkMntPoint),
		"mountRootfs":      logConfigValue(conf.MountRootfs),
	}).WithField("source", uruncJSONFilename).Debug("urunc annotations")

	return &conf, nil
}

func logConfigValue(s string) string {
	decoded, err := decodeConfigValue(s)
	if err != nil {
		uniklog.WithError(err).Errorf("Failed to decode string: %s", s)
		return s
	}
	return decoded
}

func decodeConfigValue(s string) (string, error) {
	if s == "" {
		return s, nil
	}
	
	// 1. Primary path: Strict prefix checking
	if strings.HasPrefix(s, "b64:") {
		encodedStr := strings.TrimPrefix(s, "b64:")
		decoded, err := base64.StdEncoding.DecodeString(encodedStr)
		if err != nil {
			return "", fmt.Errorf("invalid base64 encoding for prefixed string: %w", err)
		}
		return string(decoded), nil
	}

	// 2. Legacy fallback for old bunny versions emitting raw base64.
	// WARNING: This carries a documented risk of silent data corruption if a short
	// plaintext config label happens to be valid base64 and decodes to valid UTF-8.
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err == nil && utf8.Valid(decoded) {
		return string(decoded), nil
	}

	// 3. No prefix and not valid raw base64, return exactly as plaintext
	return s, nil
}

// decode decodes the base64 encoded values of the Unikernel config, safely falling back to plaintext
func (c *UnikernelConfig) decode() error {
	var err error
	if c.UnikernelCmd, err = decodeConfigValue(c.UnikernelCmd); err != nil {
		return err
	}
	if c.Hypervisor, err = decodeConfigValue(c.Hypervisor); err != nil {
		return err
	}
	if c.UnikernelType, err = decodeConfigValue(c.UnikernelType); err != nil {
		return err
	}
	if c.UnikernelVersion, err = decodeConfigValue(c.UnikernelVersion); err != nil {
		return err
	}
	if c.UnikernelBinary, err = decodeConfigValue(c.UnikernelBinary); err != nil {
		return err
	}
	if c.Initrd, err = decodeConfigValue(c.Initrd); err != nil {
		return err
	}
	if c.Block, err = decodeConfigValue(c.Block); err != nil {
		return err
	}
	if c.BlkMntPoint, err = decodeConfigValue(c.BlkMntPoint); err != nil {
		return err
	}
	if c.MountRootfs, err = decodeConfigValue(c.MountRootfs); err != nil {
		return err
	}
	return nil
}

// Map returns a map containing the Unikernel config data
func (c *UnikernelConfig) Map() map[string]string {
	myMap := make(map[string]string)
	if c.UnikernelCmd != "" {
		myMap[annotCmdLine] = c.UnikernelCmd
	}
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

	return myMap
}
