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

package containerdshim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/log"
	"github.com/containerd/ttrpc"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	containerdShim "github.com/urunc-dev/urunc/pkg/containerd-shim/containerd"
)

// taskService is urunc's shim-side wrapper around containerd's runc task
// service. It wires urunc task setup before forwarding calls to the wrapped
// service.
type taskService struct {
	taskAPI.TaskService

	containerdAddress string
}

func (s *taskService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	spec, mode, specErr := readSpec(r.Bundle)

	session, err := containerdShim.OpenSession(ctx, s.containerdAddress, r.ID)
	if err != nil {
		log.G(ctx).WithError(err).Warn("urunc(shim): failed to open containerd session")
	} else {
		defer func() {
			if err := session.Close(); err != nil {
				log.G(ctx).WithError(err).Warn("urunc(shim): failed to close containerd session")
			}
		}()
		if specErr != nil {
			log.G(ctx).WithError(specErr).Warn("urunc(shim): failed to inject annotations to spec")
		} else {
			changed, err := containerdShim.InjectUruncAnnotations(ctx, session, spec)
			if err != nil {
				log.G(ctx).WithError(err).Warn("urunc(shim): failed to inject annotations to spec")
			} else if changed {
				if err := writeSpec(r.Bundle, spec, mode); err != nil {
					log.G(ctx).WithError(err).Warn("urunc(shim): failed to inject annotations to spec")
				}
			}
		}
	}

	resp, err := s.TaskService.Create(ctx, r)
	if err != nil {
		return resp, err
	}

	// ChooseRootfs after inner task Create so bundle rootfs is mounted;
	// params are persisted in bundle config.json for runtime Exec.
	if specErr != nil {
		err := fmt.Errorf("%w: %w", errGuestRootfsChoiceSkipped, specErr)
		log.G(ctx).WithError(err).Debug("urunc(shim): guest rootfs choice skipped")
		return resp, nil
	}

	changed, err := chooseGuestRootfs(r.Bundle, spec)
	if err != nil {
		if errors.Is(err, errGuestRootfsChoiceSkipped) {
			log.G(ctx).WithError(err).Debug("urunc(shim): guest rootfs choice skipped")
			return resp, nil
		}
		log.G(ctx).WithError(err).Warn("urunc(shim): failed to choose guest rootfs")
		return nil, err
	}
	if changed {
		if err := writeSpec(r.Bundle, spec, mode); err != nil {
			log.G(ctx).WithError(err).Warn("urunc(shim): failed to choose guest rootfs")
			return nil, err
		}
	}

	return resp, nil
}

func (s *taskService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	return s.TaskService.Delete(ctx, r)
}

func (s *taskService) RegisterTTRPC(server *ttrpc.Server) error {
	taskAPI.RegisterTaskService(server, s)
	return nil
}

func readSpec(bundle string) (*specs.Spec, os.FileMode, error) {
	configPath := filepath.Join(bundle, "config.json")
	info, err := os.Stat(configPath)
	if err != nil {
		return nil, 0, fmt.Errorf("stat config.json: %w", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, 0, fmt.Errorf("read config.json: %w", err)
	}

	var spec specs.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, 0, fmt.Errorf("unmarshal config.json: %w", err)
	}

	return &spec, info.Mode(), nil
}

func writeSpec(bundle string, spec *specs.Spec, mode os.FileMode) error {
	configPath := filepath.Join(bundle, "config.json")
	patched, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config.json: %w", err)
	}

	return atomicWriteFile(configPath, patched, mode)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmpDir := filepath.Dir(path)

	f, err := os.CreateTemp(tmpDir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}

	tmpName := f.Name()
	defer os.Remove(tmpName)

	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		return err
	}

	return nil
}
