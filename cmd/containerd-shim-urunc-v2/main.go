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

package main

import (
	"context"
	"io"
	"os"
	"sync"

	bootapi "github.com/containerd/containerd/api/runtime/bootstrap/v1"
	"github.com/containerd/containerd/v2/cmd/containerd-shim-runc-v2/manager"
	"github.com/containerd/containerd/v2/pkg/protobuf/proto"
	"github.com/containerd/containerd/v2/pkg/shim"
	_ "github.com/urunc-dev/urunc/pkg/containerd-shim"
)

func main() {
	var isStart bool
	for _, arg := range os.Args {
		if arg == "start" {
			isStart = true
			break
		}
	}

	if isStart {
		r, w, err := os.Pipe()
		if err == nil {
			oldStdout := os.Stdout
			os.Stdout = w

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				data, _ := io.ReadAll(r)
				var result bootapi.BootstrapResult
				if err := proto.Unmarshal(data, &result); err == nil && result.Address != "" {
					oldStdout.WriteString(result.Address)
				} else {
					oldStdout.Write(data)
				}
			}()

			shim.RunShim(context.Background(), manager.NewShimManager("io.containerd.urunc.v2"))

			os.Stdout = oldStdout
			w.Close()
			wg.Wait()
			return
		}
	}

	shim.RunShim(context.Background(), manager.NewShimManager("io.containerd.urunc.v2"))
}
