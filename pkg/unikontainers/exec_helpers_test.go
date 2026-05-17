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
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func makeTestUnikontainer(vmmType string, memMB uint, vcpus uint) *Unikontainer {
	cfg := defaultUruncConfig()
	cfg.Monitors[vmmType] = types.MonitorConfig{
		DefaultMemoryMB: memMB,
		DefaultVCPUs:    vcpus,
	}
	return &Unikontainer{
		State: &specs.State{
			ID: "container123",
			Annotations: map[string]string{},
		},
		Spec: &specs.Spec{
			Process: &specs.Process{
				Args: []string{"/bin/sh", "-c", "echo hi"},
				Env:  []string{"FOO=bar"},
				Cwd:  "/",
				User: specs.User{UID: 1000, GID: 1000},
			},
			Linux: &specs.Linux{
				Seccomp: &specs.LinuxSeccomp{},
			},
		},
		UruncCfg: cfg,
	}
}

func TestBuildVMMArgs(t *testing.T) {
	t.Run("default memory and vcpus", func(t *testing.T) {
		t.Parallel()
		u := makeTestUnikontainer("qemu", 256, 2)
		args, err := u.buildVMMArgs("qemu", "/bin/unikernel", "/tmp/initrd")
		assert.NoError(t, err)
		assert.Equal(t, "container123", args.ContainerID)
		assert.Equal(t, "/bin/unikernel", args.UnikernelPath)
		assert.Equal(t, "/tmp/initrd", args.InitrdPath)
		assert.Equal(t, uint64(256*1024*1024), args.MemSizeB)
		assert.Equal(t, uint(2), args.VCPUs)
		assert.True(t, args.Seccomp, "seccomp should be enabled by default")
	})

	t.Run("seccomp disabled when nil", func(t *testing.T) {
		t.Parallel()
		u := makeTestUnikontainer("qemu", 256, 2)
		u.Spec.Linux.Seccomp = nil
		args, err := u.buildVMMArgs("qemu", "/bin/unikernel", "")
		assert.NoError(t, err)
		assert.False(t, args.Seccomp, "seccomp should be disabled")
	})

	t.Run("memory limit from spec overrides default", func(t *testing.T) {
		t.Parallel()
		u := makeTestUnikontainer("qemu", 256, 2)
		limit := int64(512 * 1024 * 1024)
		u.Spec.Linux.Resources = &specs.LinuxResources{
			Memory: &specs.LinuxMemory{Limit: &limit},
		}
		args, err := u.buildVMMArgs("qemu", "/bin/unikernel", "")
		assert.NoError(t, err)
		assert.Equal(t, uint64(limit), args.MemSizeB)
	})

	t.Run("vcpus defaults to 1 if zero", func(t *testing.T) {
		t.Parallel()
		u := makeTestUnikontainer("qemu", 256, 0)
		args, err := u.buildVMMArgs("qemu", "/bin/unikernel", "")
		assert.NoError(t, err)
		assert.Equal(t, uint(1), args.VCPUs, "zero vcpus should default to 1")
	})
}

func TestBuildUnikernelParams(t *testing.T) {
	t.Run("cmdline from spec args", func(t *testing.T) {
		t.Parallel()
		u := makeTestUnikontainer("qemu", 256, 2)
		params := u.buildUnikernelParams("qemu", "0.1")
		assert.Equal(t, []string{"/bin/sh", "-c", "echo hi"}, params.CmdLine)
		assert.Equal(t, []string{"FOO=bar"}, params.EnvVars)
		assert.Equal(t, "qemu", params.Monitor)
		assert.Equal(t, "0.1", params.Version)
		assert.Equal(t, uint32(1000), params.ProcConf.UID)
		assert.Equal(t, uint32(1000), params.ProcConf.GID)
	})

	t.Run("cmdline falls back to annotation", func(t *testing.T) {
		t.Parallel()
		u := makeTestUnikontainer("qemu", 256, 2)
		u.Spec.Process.Args = nil
		u.State.Annotations[annotCmdLine] = "echo hello world"
		params := u.buildUnikernelParams("qemu", "")
		assert.Equal(t, []string{"echo", "hello", "world"}, params.CmdLine)
	})

	t.Run("empty cmdline and no annotation", func(t *testing.T) {
		t.Parallel()
		u := makeTestUnikontainer("qemu", 256, 2)
		u.Spec.Process.Args = nil
		params := u.buildUnikernelParams("qemu", "")
		assert.Empty(t, params.CmdLine)
	})
}
