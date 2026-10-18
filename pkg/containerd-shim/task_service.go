//go:build linux

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
	registerTaskV2(server, s)
	return nil
}

// registerTaskV2 serves the v3 handlers under the task API v2 name for
// containerd < 2.3 (see RunShim). Both APIs have identical messages.
//
// TODO: Remove together with compat_shim.go, see RunShim.
func registerTaskV2(server *ttrpc.Server, svc taskAPI.TTRPCTaskService) {
	server.RegisterService("containerd.task.v2.Task", &ttrpc.ServiceDesc{
		Methods: map[string]ttrpc.Method{
			"State":      taskV2Method(svc.State),
			"Create":     taskV2Method(svc.Create),
			"Start":      taskV2Method(svc.Start),
			"Delete":     taskV2Method(svc.Delete),
			"Pids":       taskV2Method(svc.Pids),
			"Pause":      taskV2Method(svc.Pause),
			"Resume":     taskV2Method(svc.Resume),
			"Checkpoint": taskV2Method(svc.Checkpoint),
			"Kill":       taskV2Method(svc.Kill),
			"Exec":       taskV2Method(svc.Exec),
			"ResizePty":  taskV2Method(svc.ResizePty),
			"CloseIO":    taskV2Method(svc.CloseIO),
			"Update":     taskV2Method(svc.Update),
			"Wait":       taskV2Method(svc.Wait),
			"Stats":      taskV2Method(svc.Stats),
			"Connect":    taskV2Method(svc.Connect),
			"Shutdown":   taskV2Method(svc.Shutdown),
		},
	})
}

// taskV2Method decodes a v2 request directly into the v3 request type.
func taskV2Method[Req any, Resp any](fn func(context.Context, *Req) (Resp, error)) ttrpc.Method {
	return func(ctx context.Context, unmarshal func(any) error) (any, error) {
		var req Req
		err := unmarshal(&req)
		if err != nil {
			return nil, err
		}
		return fn(ctx, &req)
	}
}
