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

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urunc-dev/urunc/pkg/unikontainers/initrd"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

// TODO: Find and set the correct size for the tmpfs in the host
const tmpfsSizeForInitrdRootfs = "65536k"

type initrdRootfs struct {
	mounts             []specs.Mount
	monRootfs          string
	initrdHostFullPath string
}

func (i initrdRootfs) preSetup() error {
	return nil
}

func (i initrdRootfs) postSetup() error {
	err := initrd.CopyFileMountsToInitrd(i.initrdHostFullPath, i.mounts)
	if err != nil {
		return fmt.Errorf("failed to update guest's initrd: %w", err)
	}

	err = createTmpfs(i.monRootfs, "/tmp",
		unix.MS_NOSUID|unix.MS_NOEXEC|unix.MS_STRICTATIME,
		"1777", tmpfsSizeForInitrdRootfs)
	if err != nil {
		err = fmt.Errorf("failed to create tmpfs for monitor's execution environment: %w", err)
	}

	return err
}

func (i initrdRootfs) getBlockDevs() ([]types.BlockDevParams, error) {
	return nil, nil
}

// TODO: Return an array instead of a single struct
func (i initrdRootfs) getSharedDirs() (types.SharedfsParams, error) {
	return types.SharedfsParams{}, nil
}

func (i initrdRootfs) preStart() error {
	return nil
}
