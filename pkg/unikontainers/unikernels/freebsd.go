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

package unikernels

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const (
	FreeBSDUnikernel string = "freebsd"
	netStartMarker   string = "UNS" // Net configuration start marker
	netEndMarker     string = "UNE" // Net configuration end marker
	paddingMarker    string = "PAD" // Padding bytes
	freebsdRootBlock string = "/dev/vtbd0"
	// blockSectorSize is the granularity at which a raw file is exposed by
	// the monitor as a virtio-blk device. Firecracker silently hides a trailing
	// partial sector, so the config file is padded to a multiple of it.
	blockSectorSize int    = 512
	freeBSDufs      string = "ufs"
	freeBSDext2fs   string = "ext2fs"
	p9MountTag      string = "fs0"

	fsTypeUFS  string = "ufs"
	fsTypeExt2 string = "ext2"
	fsTypeExt3 string = "ext3"
	fsTypeExt4 string = "ext4"
)

// UFS superblock fields (struct fs in FreeBSD's sys/ufs/ffs/fs.h).
const (
	ufsSuperblockSize       int    = 1376
	ufs1Magic               uint32 = 0x00011954
	ufs2Magic               uint32 = 0x19540119
	ufsGroupCountOffset     int    = 44
	ufsBlockSizeOffset      int    = 48
	ufsFragSizeOffset       int    = 52
	ufsFragsPerBlockOffset  int    = 56
	ufsSuperblockSizeOffset int    = 104
	ufsMagicOffset          int    = 1372
	ufsSuperblockUFS1       int64  = 8192
	ufsSuperblockUFS2       int64  = 65536
	ufsMinBlockSize         uint32 = 4096
	ufsMaxBlockSize         uint32 = 65536
	ufsMinFragSize          uint32 = 512
	ufsMaxFrag              uint32 = 8
	ufsMaxSuperblockSize    uint32 = 8192
)

// ext superblock and feature flags (as in the ext probe of util-linux).
const (
	extSuperblockOffset       int64  = 1024
	extSuperblockSize         int    = 1024
	extMagic                  uint16 = 0xEF53
	extMagicOffset            int    = 56
	extCompatibleOffset       int    = 92
	extIncompatibleOffset     int    = 96
	extReadOnlyOffset         int    = 100
	extCompatibleJournal      uint32 = 0x0004
	extIncompatibleJournalDev uint32 = 0x0008
	extReadOnlySupported      uint32 = 0x0007 // sparse_super, large_file, btree_dir
	ext2IncompatibleSupported uint32 = 0x0012 // filetype, meta_bg
	ext3IncompatibleSupported uint32 = 0x0016 // ext2's and needs_recovery
)

// The order in which FreeBSD searches for the superblock (sys/ufs/ffs/fs.h).
var ufsSuperblockLocations = []int64{ufsSuperblockUFS2, ufsSuperblockUFS1, 0, 262144}

type FreeBSD struct {
	Command     []string
	InitPath    string
	WithUrunit  bool
	Monitor     string
	Env         []string
	RootFsType  string
	BlockFsType string
	Net         FreeBSDNet
	Block       []types.BlockDevParams
	ProcConfig  types.ProcessConfig
}

type FreeBSDNet struct {
	Address string
	Gateway string
	Mask    string
}

// CommandString builds the FreeBSD boot arguments. On the PVH boot mode every
// name=value token becomes a kernel environment variable. Therefore, use this
// mechanism to pass the location of the urunit configuration to the guest.
// Boot arguments must not contain commas, which act as token delimiters.
func (f *FreeBSD) CommandString() (string, error) {
	var params []string

	if f.RootFsType == "9pfs" {
		params = append(params, "vfs.root.mountfrom=p9fs:"+p9MountTag)
	} else {
		params = append(params, "vfs.root.mountfrom="+f.BlockFsType+":"+freebsdRootBlock)
	}

	if f.InitPath != "" {
		params = append(params, "init_path="+f.InitPath)
	}

	// If init is urunit then set the URUNIT_* variables
	if f.WithUrunit {
		// urunit reads its configuration from a raw virtio-blk device
		// attached after all other block devices. In the case of
		// block-based rootfs the other block devices are the rootfs
		// and the block-based volumes. In the case of 9pfs-based
		// rootfs there is no other block device and therefore
		// len(f.Block) will be 0.
		params = append(params, "URUNIT_CONFIG=/dev/vtbd"+strconv.Itoa(len(f.Block)))
		if f.Net.Address != "" {
			ln := LinuxNet{Address: f.Net.Address, Gateway: f.Net.Gateway, Mask: f.Net.Mask}
			if !IsIPInSubnet(ln) {
				params = append(params, "URUNIT_DEFROUTE=1")
			}
		}
	}

	return strings.Join(params, " "), nil
}

func (f *FreeBSD) SupportsBlock() bool {
	return true
}

func (f *FreeBSD) SupportsFS(fsType string) bool {
	switch fsType {
	case "ext2":
		return true
	case "9pfs":
		return true
	default:
		return false
	}
}

func (f *FreeBSD) MonitorNetCli(ifName string, mac string) []string {
	switch f.Monitor {
	case "qemu":
		// QEMU microvm has no PCI bus: use the virtio-mmio transport.
		return []string{
			"-netdev", "tap,id=net0,script=no,downscript=no,ifname=" + ifName,
			"-device", "virtio-net-device,netdev=net0,mac=" + mac,
		}
	default:
		return nil
	}
}

func (f *FreeBSD) MonitorBlockCli() []types.MonitorBlockArgs {
	// When urunit is the init, its configuration is passed as one extra block
	// device after a block-based rootfs and block-based volumes. This holds
	// for a 9pfs rootfs too: the rootfs is served over 9p, but the urunit
	// config is still a raw virtio-blk device.
	blockArgsCap := len(f.Block)
	if f.WithUrunit {
		blockArgsCap++
	}
	if blockArgsCap == 0 {
		return nil
	}
	blkArgs := make([]types.MonitorBlockArgs, 0, blockArgsCap)
	switch f.Monitor {
	case "qemu":
		for _, aBlock := range f.Block {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ExactArgs: qemuMMIOBlock(aBlock.ID, aBlock.Source),
			})
		}
		if f.WithUrunit {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ExactArgs: qemuMMIOBlock("urunit", urunitConfPath),
			})
		}
	case "firecracker":
		for _, aBlock := range f.Block {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ID:   "FC" + aBlock.ID,
				Path: aBlock.Source,
			})
		}
		if f.WithUrunit {
			blkArgs = append(blkArgs, types.MonitorBlockArgs{
				ID:   "FC_URUNIT_CONFIG",
				Path: urunitConfPath,
			})
		}
	default:
		return nil
	}

	return blkArgs
}

// MonitorSharedfsCli exposes the 9p shared rootfs over the virtio-mmio
// transport only for Qemu's microvm.  It sets multidevs=remap: since the
// shared container rootfs usually spans several host devices (overlay layers,
// bind-mounted files and dirs).
func (f *FreeBSD) MonitorSharedfsCli(fsType string, path string) []string {
	if f.Monitor != "qemu" || fsType != "9pfs" {
		return nil
	}
	fsdev := "local,id=rootfs9p,security_model=none,multidevs=remap,path=" + path
	dev := "virtio-9p-device,fsdev=rootfs9p,mount_tag=" + p9MountTag
	return []string{"-fsdev", fsdev, "-device", dev}
}

func (f *FreeBSD) MonitorCli() types.MonitorCliArgs {
	switch f.Monitor {
	case "qemu":
		// NOTE: pic=off makes QEMU (8.2/9.1) crash right after FreeBSD
		// initializes the I/O APIC. ACPI must be on, since FreeBSD uses
		// the ACPI tables instead of the (deprecated) MP table.
		monArgs := []string{"-M", "microvm,acpi=on,pic=on,pit=on,rtc=on", "-nodefaults", "-no-reboot"}
		return types.MonitorCliArgs{
			OtherArgs: monArgs,
		}
	default:
		return types.MonitorCliArgs{}
	}
}

func (f *FreeBSD) Init(data types.UnikernelParams) error {
	// if Mask is empty, there is no network
	if data.Net.Mask != "" {
		f.Net.Address = data.Net.IP
		f.Net.Gateway = data.Net.Gateway
		f.Net.Mask = data.Net.Mask
	}
	f.Block = data.Block
	f.Env = data.EnvVars
	f.WithUrunit = usesUrunit(data.CmdLine)
	// init_path is the first command token verbatim
	if len(data.CmdLine) > 0 {
		f.InitPath = data.CmdLine[0]
		f.Command = data.CmdLine[1:]
	}
	f.Monitor = data.Monitor
	f.ProcConfig = data.ProcConf
	f.RootFsType = data.Rootfs.Type
	switch f.RootFsType {
	case "block":
		mountType, err := blockRootfsMountType(data.Block)
		if err != nil {
			return err
		}
		f.BlockFsType = mountType
	case "9pfs":
		// The rootfs is served over 9p; nothing extra to configure here.
	default:
		return fmt.Errorf("freebsd: unsupported rootfs type %q: only block and 9pfs are supported", f.RootFsType)
	}

	// Only provision the urunit configuration when urunit is the init.
	if f.WithUrunit {
		err := f.setupUrunitConfig()
		if err != nil {
			return err
		}
	}

	return nil
}

// blockRootfsMountType returns the filesystem name for vfs.root.mountfrom.
// It reads the rootfs, so it must run after the pivot into the monitor rootfs.
func blockRootfsMountType(blocks []types.BlockDevParams) (string, error) {
	for _, b := range blocks {
		if b.ID != "rootfs" {
			continue
		}
		fsType, err := detectRootfsType(b.Source)
		if err != nil {
			return "", fmt.Errorf("freebsd: failed to read the block rootfs: %w", err)
		}
		switch fsType {
		case fsTypeUFS:
			return freeBSDufs, nil
		case fsTypeExt2:
			return freeBSDext2fs, nil
		case "":
			return "", errors.New("freebsd: could not recognize the filesystem of the block rootfs")
		default:
			return "", fmt.Errorf("freebsd: unsupported block rootfs filesystem %q: only ufs and ext2 are supported", fsType)
		}
	}

	return "", errors.New("freebsd: no block device found for the block rootfs")
}

// detectRootfsType returns the filesystem of a block image or device, or an
// empty string if it is not recognized.
func detectRootfsType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	for _, location := range ufsSuperblockLocations {
		sb, err := readSuperblock(f, location, ufsSuperblockSize)
		if err != nil {
			return "", err
		}
		if isUFSSuperblock(sb, location) {
			return fsTypeUFS, nil
		}
	}

	sb, err := readSuperblock(f, extSuperblockOffset, extSuperblockSize)
	if err != nil {
		return "", err
	}

	return extFsType(sb), nil
}

// readSuperblock returns nil if the file is too short to hold size bytes at offset.
func readSuperblock(f *os.File, offset int64, size int) ([]byte, error) {
	sb := make([]byte, size)
	_, err := f.ReadAt(sb, offset)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return sb, nil
}

// isUFSSuperblock applies a subset of the FreeBSD kernel's superblock checks.
// The kernel accepts UFS2 only at 65536 or 0, and UFS1 only up to 8192.
func isUFSSuperblock(sb []byte, location int64) bool {
	if sb == nil {
		return false
	}
	field := func(offset int) uint32 {
		return binary.LittleEndian.Uint32(sb[offset:])
	}

	switch field(ufsMagicOffset) {
	case ufs2Magic:
		if location != ufsSuperblockUFS2 && location != 0 {
			return false
		}
	case ufs1Magic:
		if location > ufsSuperblockUFS1 {
			return false
		}
	default:
		return false
	}

	blockSize, fragSize, frags := field(ufsBlockSizeOffset), field(ufsFragSizeOffset), field(ufsFragsPerBlockOffset)
	superblockSize := field(ufsSuperblockSizeOffset)

	return field(ufsGroupCountOffset) >= 1 &&
		isPowerOfTwo(blockSize) && blockSize >= ufsMinBlockSize && blockSize <= ufsMaxBlockSize &&
		isPowerOfTwo(fragSize) && fragSize >= ufsMinFragSize &&
		frags >= 1 && frags <= ufsMaxFrag && fragSize*frags == blockSize &&
		superblockSize >= uint32(ufsSuperblockSize) && superblockSize <= ufsMaxSuperblockSize
}

func isPowerOfTwo(v uint32) bool {
	return v != 0 && v&(v-1) == 0
}

// extFsType uses the feature flags, as ext2, ext3 and ext4 share one magic number.
// The rules are those of the ext probe in util-linux.
func extFsType(sb []byte) string {
	if sb == nil || binary.LittleEndian.Uint16(sb[extMagicOffset:]) != extMagic {
		return ""
	}
	compatible := binary.LittleEndian.Uint32(sb[extCompatibleOffset:])
	incompatible := binary.LittleEndian.Uint32(sb[extIncompatibleOffset:])
	readOnly := binary.LittleEndian.Uint32(sb[extReadOnlyOffset:])

	// An external journal holds no filesystem to boot from.
	if incompatible&extIncompatibleJournalDev != 0 {
		return ""
	}
	if readOnly&^extReadOnlySupported == 0 {
		if compatible&extCompatibleJournal == 0 && incompatible&^ext2IncompatibleSupported == 0 {
			return fsTypeExt2
		}
		if compatible&extCompatibleJournal != 0 && incompatible&^ext3IncompatibleSupported == 0 {
			return fsTypeExt3
		}
	}

	return fsTypeExt4
}

// usesUrunit reports whether the entrypoint of the container was urunit.
// It does that simply checking if the command to execute is urunit.
func usesUrunit(cmdLine []string) bool {
	return len(cmdLine) > 0 && filepath.Base(cmdLine[0]) == "urunit"
}

func qemuMMIOBlock(id string, path string) []string {
	return []string{
		"-device", fmt.Sprintf("virtio-blk-device,serial=%s,drive=%s", id, id),
		"-drive", fmt.Sprintf("format=raw,if=none,id=%s,file=%s", id, path),
	}
}

// setupUrunitConfig writes the urunit configuration file into the monitor
// rootfs at urunitConfPath. The monitor exposes that file to the guest as a
// raw virtio-blk device.
func (f *FreeBSD) setupUrunitConfig() error {
	urunitConfig := f.buildUrunitConfig()
	err := createFile(urunitConfPath, urunitConfig)
	if err != nil {
		return fmt.Errorf("failed to setup urunit config: %w", err)
	}
	return nil
}

// buildUrunitConfig creates the configuration content for urunit. Compared to
// Linux guests it also carries the application command (ARC/ARV, since the
// FreeBSD kernel starts init without arguments) and the network configuration
// (UNS/UNE, since FreeBSD has no ip= boot parameter). The result is padded to
// a multiple of the sector size, as it is exposed as a raw block device.
func (f *FreeBSD) buildUrunitConfig() string {
	// Format: UES\n<env1>\n<env2>\n...\nUEE\n
	var sb strings.Builder
	sb.WriteString(envStartMarker)
	sb.WriteString("\n")
	if len(f.Env) > 0 {
		sb.WriteString(strings.Join(f.Env, "\n"))
		sb.WriteString("\n")
	}
	sb.WriteString(envEndMarker)
	sb.WriteString("\n")
	sb.WriteString(lpcStartMarker)
	sb.WriteString("\n")
	sb.WriteString("UID:")
	sb.WriteString(strconv.FormatUint(uint64(f.ProcConfig.UID), 10))
	sb.WriteString("\n")
	sb.WriteString("GID:")
	sb.WriteString(strconv.FormatUint(uint64(f.ProcConfig.GID), 10))
	sb.WriteString("\n")
	sb.WriteString("WD:")
	sb.WriteString(f.ProcConfig.WorkDir)
	sb.WriteString("\n")
	sb.WriteString("ARC:")
	sb.WriteString(strconv.FormatUint(uint64(len(f.Command)), 10))
	sb.WriteString("\n")
	for _, c := range f.Command {
		sb.WriteString("ARV:")
		sb.WriteString(c)
		sb.WriteString("\n")
	}
	sb.WriteString(lpcEndMarker)
	sb.WriteString("\n")
	sb.WriteString(blkStartMarker)
	sb.WriteString("\n")
	for _, b := range f.Block {
		if b.ID == "rootfs" {
			continue
		}
		sb.WriteString("ID:")
		if f.Monitor == "firecracker" {
			sb.WriteString("FC")
		}
		sb.WriteString(b.ID)
		sb.WriteString("\n")
		sb.WriteString("MP:")
		sb.WriteString(b.MountPoint)
		sb.WriteString("\n")
	}
	sb.WriteString(blkEndMarker)
	sb.WriteString("\n")
	sb.WriteString(netStartMarker)
	sb.WriteString("\n")
	sb.WriteString("IP:")
	sb.WriteString(f.Net.Address)
	sb.WriteString("\n")
	sb.WriteString("GW:")
	sb.WriteString(f.Net.Gateway)
	sb.WriteString("\n")
	sb.WriteString("MSK:")
	sb.WriteString(f.Net.Mask)
	sb.WriteString("\n")
	sb.WriteString(netEndMarker)
	sb.WriteString("\n")
	// Pad to a whole number of sectors. The PAD line marks the end of the
	// configuration for readers; the remaining bytes are newlines.
	sb.WriteString(paddingMarker)
	sb.WriteString("\n")
	for sb.Len()%blockSectorSize != 0 {
		sb.WriteString("\n")
	}
	return sb.String()
}

func newFreeBSD() *FreeBSD {
	freebsdStruct := new(FreeBSD)
	return freebsdStruct
}
