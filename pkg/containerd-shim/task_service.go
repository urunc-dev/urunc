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

	taskAPI "github.com/containerd/containerd/api/runtime/task/v3"
	"github.com/containerd/log"
	"github.com/containerd/ttrpc"
	containerdShim "github.com/urunc-dev/urunc/pkg/containerd-shim/containerd"
)

// taskService is urunc's shim-side wrapper around containerd's runc task
// service. It wires urunc task setup before forwarding calls to the wrapped
// service.
type taskService struct {
	taskAPI.TTRPCTaskService

	containerdAddress string
}

func (s *taskService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	session, err := containerdShim.OpenSession(ctx, s.containerdAddress, r.ID)
	if err != nil {
		log.G(ctx).WithError(err).Warn("urunc(shim): failed to open containerd session")
	} else {
		defer func() {
			if err := session.Close(); err != nil {
				log.G(ctx).WithError(err).Warn("urunc(shim): failed to close containerd session")
			}
		}()
		if err := containerdShim.InjectUruncAnnotations(ctx, session, r.Bundle); err != nil {
			log.G(ctx).WithError(err).Warn("urunc(shim): failed to inject annotations to spec")
		}
	}

	resp, err := s.TTRPCTaskService.Create(ctx, r)
	if err != nil {
		return resp, err
	}

	// TODO: #816 - Restore rootfs choice here once shim integration is complete.
	// For now, rootfs is selected during urunc create (InitialSetup phase).
	// ChooseRootfs after inner task Create so bundle rootfs is mounted;
	// params are persisted in bundle config.json for runtime Exec.
	// if err := chooseGuestRootfs(r); err != nil {
	// 	if errors.Is(err, errGuestRootfsChoiceSkipped) {
	// 		log.G(ctx).WithError(err).Debug("urunc(shim): guest rootfs choice skipped")
	// 		return resp, nil
	// 	}
	// 	log.G(ctx).WithError(err).Warn("urunc(shim): failed to choose guest rootfs")
	// 	return nil, err
	// }

	return resp, nil
}

func (s *taskService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	return s.TTRPCTaskService.Delete(ctx, r)
}

func (s *taskService) RegisterTTRPC(server *ttrpc.Server) error {
	taskAPI.RegisterTTRPCTaskService(server, s)

	// Register v2 compatibility so containerd 1.7.x daemon can connect
	server.RegisterService("containerd.task.v2.Task", &ttrpc.ServiceDesc{
		Methods: map[string]ttrpc.Method{
			"State": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.StateRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.State(ctx, &req)
			},
			"Create": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.CreateTaskRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Create(ctx, &req)
			},
			"Start": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.StartRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Start(ctx, &req)
			},
			"Delete": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.DeleteRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Delete(ctx, &req)
			},
			"Pids": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.PidsRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Pids(ctx, &req)
			},
			"Pause": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.PauseRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Pause(ctx, &req)
			},
			"Resume": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.ResumeRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Resume(ctx, &req)
			},
			"Checkpoint": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.CheckpointTaskRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Checkpoint(ctx, &req)
			},
			"Kill": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.KillRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Kill(ctx, &req)
			},
			"Exec": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.ExecProcessRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Exec(ctx, &req)
			},
			"ResizePty": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.ResizePtyRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.ResizePty(ctx, &req)
			},
			"CloseIO": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.CloseIORequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.CloseIO(ctx, &req)
			},
			"Update": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.UpdateTaskRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Update(ctx, &req)
			},
			"Wait": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.WaitRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Wait(ctx, &req)
			},
			"Stats": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.StatsRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Stats(ctx, &req)
			},
			"Connect": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.ConnectRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Connect(ctx, &req)
			},
			"Shutdown": func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
				var req taskAPI.ShutdownRequest
				if err := unmarshal(&req); err != nil {
					return nil, err
				}
				return s.Shutdown(ctx, &req)
			},
		},
	})
	return nil
}
