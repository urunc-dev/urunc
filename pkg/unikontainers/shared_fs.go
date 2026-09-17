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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urunc-dev/urunc/internal/constants"
	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

// TODO: Find and set the correct size for the tmpfs in the host
const tmpfsSizeFor9pfsRootfs = "65536k"

type sharedfsRootfs struct {
	mounts      []specs.Mount
	vfsdConfig  types.ExtraBinConfig
	sharedPath  string
	mountedPath string
	sfsType     string
	memory      uint64
	// bootKernelHost and bootInitrdHost are the host kernel and boot initrd of
	// a generic container boot (the bootKernel/bootInitrd annotations). When
	// set, they are mounted read-only into the monitor rootfs so that the
	// monitor boots them in place of a kernel from the image.
	bootKernelHost string
	bootInitrdHost string
	// containerUID and containerGID are the identity the guest runs the
	// container process as (Spec.Process.User). virtiofsd maps this identity onto
	// the host owner of the shared files so a non-root guest owns its volumes
	// (see virtiofsIDTranslations).
	containerUID uint32
	containerGID uint32
}

func (s sharedfsRootfs) hasContainerBoot() bool {
	return s.bootKernelHost != "" && s.bootInitrdHost != ""
}

func (s sharedfsRootfs) preSetup() error {
	if !s.hasContainerBoot() {
		return nil
	}

	// The annotations were validated as clean absolute paths; make sure they
	// name real files now, so a typo fails at create with a clear message
	// instead of as a failed mount when the container starts.
	for key, path := range map[string]string{annotBootKernel: s.bootKernelHost, annotBootInitrd: s.bootInitrdHost} {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular file, got %s", key, path)
		}
	}

	return nil
}

func (s sharedfsRootfs) postSetup() error {
	return nil
}

func (s sharedfsRootfs) getMounts() ([]specs.Mount, error) {
	// Mount the container's rootfs inside the monitor rootfs and then the
	// container's volumes on top of it.
	mounts := []specs.Mount{bindMount(s.mountedPath, containerRootfsMountPath, true, false, "nodev", "nosuid", "noexec")}

	if s.hasContainerBoot() {
		// Generic container boot: the host kernel and boot initrd are mounted
		// read-only into the monitor rootfs, next to (not inside) the container
		// rootfs, which is shared with the guest. The guest boots a private copy
		// of the initrd (see unikernels.Linux), so the host file is never written.
		mounts = append(mounts,
			bindMount(s.bootKernelHost, constants.ContainerBootKernelPath, true, true, "nodev", "nosuid", "noexec"),
			bindMount(s.bootInitrdHost, constants.ContainerBootInitrdPath, true, true, "nodev", "nosuid", "noexec"),
		)
	}

	if s.sfsType == "virtiofs" {
		// Get the virtiofsd binary from host in monRootfs
		mounts = append(mounts, bindMount(s.vfsdConfig.Path, s.vfsdConfig.Path, true, true))
	}

	tmpfsSize := chooseTmpfsSize(s.sfsType, s.memory)
	mounts = append(mounts, tmpfsMount("/tmp", tmpfsSize))

	bindMounts, err := filterBindMounts(s.mountedPath, s.mounts)
	if err != nil {
		return nil, err
	}
	mounts = append(mounts, bindMounts...)

	return mounts, nil
}

func (s sharedfsRootfs) getBlockDevs() ([]types.BlockDevParams, error) {
	return nil, nil
}

func (s sharedfsRootfs) getSharedDirs() (types.SharedfsParams, error) {
	return types.SharedfsParams{
		Path: containerRootfsMountPath,
		Type: s.sfsType,
	}, nil
}

func (s sharedfsRootfs) preStartCmd() []string {
	if s.sfsType == "9pfs" {
		return nil
	}
	// The virtiofsd argv, with the binary itself as the first element so it can be
	// both stored in the monitor spec and spawned later by spawnProcess.
	argv := []string{
		s.vfsdConfig.Path,
		"--socket-path=/tmp/vhostqemu",
		"--shared-dir",
		s.sharedPath,
	}

	if s.vfsdConfig.Options != "" {
		argv = append(argv, strings.Fields(s.vfsdConfig.Options)...)
	}

	// Map the container's user onto the host owner of the shared files, so a
	// non-root guest process owns its bind-mounted volumes and its writes persist
	// under the host owner. A single virtiofsd shares the whole rootfs tree, so
	// there is one owner to map to: the first non-root owner among the volumes.
	argv = append(argv, virtiofsIDTranslations(bindMountOwners(s.mounts), s.containerUID, s.containerGID)...)

	return argv
}

// bindMountOwner is the host uid/gid owning the source of a bind mount that is
// shared into the guest.
type bindMountOwner struct {
	uid uint32
	gid uint32
}

// bindMountOwners stats the source of every bind mount in mounts (in the order
// they appear) and returns its host owner. A source that cannot be stat'd is
// skipped with a warning rather than failing the container, since it only costs
// the id translation, not the mount itself.
func bindMountOwners(mounts []specs.Mount) []bindMountOwner {
	var owners []bindMountOwner
	for _, m := range mounts {
		if !isBindMount(m) {
			continue
		}
		var st unix.Stat_t
		if err := unix.Stat(m.Source, &st); err != nil {
			uniklog.Warnf("virtiofs id-map: could not stat bind mount source %s: %v", m.Source, err)
			continue
		}
		owners = append(owners, bindMountOwner{uid: st.Uid, gid: st.Gid})
	}
	return owners
}

// virtiofsIDTranslations builds the virtiofsd --translate-uid/--translate-gid
// options that map the container's user (gUID/gGID, the identity the guest runs
// the container process as) onto the host owner of the shared files.
//
// One virtiofsd shares the whole container rootfs tree with the volumes on top,
// so a translation applies to the entire share and there is a single owner to
// map to. We pick the first non-root owner among the bind mount sources: the
// rootfs itself is unpacked as root, whereas a volume carries the host owner the
// user chose, so its uid/gid is the one a non-root guest's writes should land
// as. virtiofsd runs as root and therefore has the rights to perform the 1:1
// bidirectional map, spelled "map:<guest>:<host>:1" in virtiofsd 1.13+.
//
// The uid and gid are chosen independently, since a source may be owned by
// root:group or user:root. A translation is emitted only when both ends are
// non-root and they actually differ:
//   - the container's own id must be non-root: the container process runs as
//     root by default, and remapping guest root would shift the whole shared
//     rootfs (unpacked as root), not just a volume;
//   - the host owner must be non-root: a root-owned source needs no shift, and
//     with no non-root owner among the volumes there is nothing to map to.
func virtiofsIDTranslations(owners []bindMountOwner, gUID, gGID uint32) []string {
	var args []string
	if gUID != 0 {
		for _, o := range owners {
			if o.uid == 0 {
				continue
			}
			if o.uid != gUID {
				args = append(args, "--translate-uid", fmt.Sprintf("map:%d:%d:1", gUID, o.uid))
			}
			break
		}
	}
	if gGID != 0 {
		for _, o := range owners {
			if o.gid == 0 {
				continue
			}
			if o.gid != gGID {
				args = append(args, "--translate-gid", fmt.Sprintf("map:%d:%d:1", gGID, o.gid))
			}
			break
		}
	}
	return args
}

func chooseTmpfsSize(sfsType string, mem uint64) string {
	if sfsType == "9pfs" {
		return tmpfsSizeFor9pfsRootfs
	}

	// For virtiofs, Qemu and virtiofsd are using a host file
	// to share the VM's RAM and hence the size of this file
	// should be the same as guest's memory. This file will
	// be placed under /tmp and we need to mount /tmp with enough
	// memory for this.
	// However, since /tmp might be used from the monitors for other
	// things too, we add one more MB extra.
	tmpMountMem := mem + (1024 * 1024)
	tmpMountMemStr := hypervisors.BytesToStringMB(tmpMountMem) + "m"

	return tmpMountMemStr
}

// filterBindMounts filters the mounts form the container's spec keeping only the
// bind mounts and adjusts the Destination path to the mountpoint of the
// container's rootfs inside the monitor rootfs.
func filterBindMounts(containerRootfs string, mounts []specs.Mount) ([]specs.Mount, error) {
	var result []specs.Mount
	for _, m := range mounts {
		// Skip non-bind mounts
		// TODO handle other types of mounts too
		if !isBindMount(m) {
			continue
		}

		// Resolve the destination against the real container rootfs,
		// and then re-root the resolved path under the container
		// rootfs mount inside the monitor rootfs, where the mount is
		// actually applied.
		resolved, err := securejoin.SecureJoin(containerRootfs, m.Destination)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(containerRootfs, resolved)
		if err != nil {
			return nil, err
		}
		result = append(result, specs.Mount{
			Type:        "bind",
			Source:      m.Source,
			Destination: filepath.Join(containerRootfsMountPath, rel),
			Options:     m.Options,
		})
	}

	return result, nil
}
