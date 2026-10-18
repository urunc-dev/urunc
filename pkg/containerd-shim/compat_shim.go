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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	bootapi "github.com/containerd/containerd/api/runtime/bootstrap/v1"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/shim"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// RunShim runs the shim and keeps it working with containerd < 2.3, which
// neither sends protobuf bootstrap params on stdin nor parses the protobuf
// reply that containerd's shim.RunShim writes. A start from such a daemon is
// handled here, everything else by shim.RunShim.
//
// TODO: Remove this file and registerTaskV2 once containerd < 2.3 is no
// longer supported. The last such release, containerd 2.0, reaches end of
// life in March 2027.
func RunShim(ctx context.Context, manager shim.Shim) {
	params, err := legacyStartParams(os.Args[1:])
	if err == nil && params != nil {
		err = legacyStart(ctx, manager, params)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "urunc shim: %v\n", err)
		os.Exit(1)
	}
	if params == nil {
		shim.RunShim(ctx, manager)
	}
}

// legacyStartParams returns the bootstrap params for a start from
// containerd < 2.3, built from the flags and environment these daemons
// pass, or nil for any other invocation. It reads stdin to tell the daemons
// apart, so for containerd >= 2.3 it hands stdin back to shim.RunShim.
func legacyStartParams(args []string) (*bootapi.BootstrapParams, error) {
	// containerd < 2.3 starts the shim with exactly these flags. If parsing
	// fails, this is not a start from such a daemon.
	shimFlags := flag.NewFlagSet("shim", flag.ContinueOnError)
	shimFlags.SetOutput(io.Discard)
	id := shimFlags.String("id", "", "")
	namespace := shimFlags.String("namespace", "", "")
	address := shimFlags.String("address", "", "")
	publishBinary := shimFlags.String("publish-binary", "", "")
	debug := shimFlags.Bool("debug", false, "")
	err := shimFlags.Parse(args)
	if err != nil || shimFlags.Arg(0) != "start" {
		return nil, nil
	}

	input, err := io.ReadAll(io.LimitReader(os.Stdin, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	// Only containerd >= 2.3 sends its version.
	var params bootapi.BootstrapParams
	err = proto.Unmarshal(input, &params)
	if err == nil && params.GetContainerdVersion() != "" {
		return nil, replaceStdin(input)
	}

	params = bootapi.BootstrapParams{
		InstanceID:             *id,
		Namespace:              *namespace,
		ContainerdBinary:       *publishBinary,
		ContainerdTtrpcAddress: os.Getenv("TTRPC_ADDRESS"),
		ContainerdGrpcAddress:  *address,
	}
	if *debug {
		params.LogLevel = bootapi.LogLevel_LOG_LEVEL_DEBUG
	}
	// containerd < 2.3 sends the runtime options, if any, as a protobuf Any.
	// Like containerd, skip them if they cannot be decoded.
	if len(input) > 0 {
		opts := &anypb.Any{}
		err = proto.Unmarshal(input, opts)
		if err == nil {
			err = params.AddExtension(opts)
			if err != nil {
				return nil, fmt.Errorf("add runtime options: %w", err)
			}
		}
	}
	return &params, nil
}

// legacyStart starts the shim server like shim.RunShim does, but replies in
// the JSON format of containerd < 2.3, announcing task API v2.
func legacyStart(ctx context.Context, manager shim.Shim, params *bootapi.BootstrapParams) error {
	ctx = namespaces.WithNamespace(ctx, params.Namespace)
	result, err := manager.Start(ctx, params)
	if err != nil {
		return err
	}
	// containerd 1.7 reconnects to running shims after a restart through the
	// address file in the bundle, the working directory here. The runc
	// manager of containerd 1.7 wrote it, but the current one does not.
	err = os.WriteFile("address", []byte(result.Address), 0o600)
	if err != nil {
		return fmt.Errorf("write address file: %w", err)
	}
	reply, err := json.Marshal(map[string]any{"version": 2, "address": result.Address, "protocol": result.Protocol})
	if err != nil {
		return fmt.Errorf("marshal reply: %w", err)
	}
	_, err = os.Stdout.Write(reply)
	return err
}

// replaceStdin makes os.Stdin read data, from an in-memory file.
func replaceStdin(data []byte) error {
	fd, err := unix.MemfdCreate("urunc-shim-stdin", unix.MFD_CLOEXEC)
	if err != nil {
		return fmt.Errorf("replace stdin: %w", err)
	}
	f := os.NewFile(uintptr(fd), "stdin")
	_, err = f.Write(data)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("replace stdin: %w", err)
	}
	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("replace stdin: %w", err)
	}
	os.Stdin = f
	return nil
}
