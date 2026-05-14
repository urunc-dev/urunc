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

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/ttrpc"
)

// taskService is urunc's shim-side wrapper around containerd's runc task
// service. It currently forwards calls to the wrapped service while keeping a
// urunc-owned place for task-level feature wiring.
type taskService struct {
	taskAPI.TaskService

	containerdAddress string
}

func (s *taskService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	return s.TaskService.Create(ctx, r)
}

func (s *taskService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	return s.TaskService.Delete(ctx, r)
}

func (s *taskService) RegisterTTRPC(server *ttrpc.Server) error {
	taskAPI.RegisterTaskService(server, s)
	return nil
}
