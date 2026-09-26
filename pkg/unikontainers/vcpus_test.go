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
	"runtime"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cpuResources(quota *int64, period *uint64, cpus string) *specs.LinuxResources {
	return &specs.LinuxResources{CPU: &specs.LinuxCPU{Quota: quota, Period: period, Cpus: cpus}}
}

func i64(v int64) *int64   { return &v }
func u64(v uint64) *uint64 { return &v }

func TestMonitorVCPUs(t *testing.T) {
	t.Parallel()
	const host = 16
	p := u64(100000)
	for _, tc := range []struct {
		name      string
		def       uint
		resources *specs.LinuxResources
		host      uint
		want      uint
	}{
		{"no resources", 2, nil, host, 2},
		{"no CPU resources", 2, &specs.LinuxResources{}, host, 2},
		{"a whole quota", 1, cpuResources(i64(400000), p, ""), host, 4},
		// nerdctl --cpus 1.5 is a quota of 150000: the guest needs 2 vCPUs.
		{"a fractional quota rounds up", 1, cpuResources(i64(150000), p, ""), host, 2},
		{"a quota below one CPU", 1, cpuResources(i64(50000), p, ""), host, 1},
		{"an unlimited quota", 2, cpuResources(i64(-1), p, ""), host, 2},
		{"a quota with no period", 2, cpuResources(i64(400000), nil, ""), host, 2},
		{"a zero period", 2, cpuResources(i64(400000), u64(0), ""), host, 2},
		{"a cpuset", 1, cpuResources(nil, nil, "0-3"), host, 4},
		{"a cpuset with a range and a single CPU", 1, cpuResources(nil, nil, "0-3,6"), host, 5},
		{"a quota above the cpuset", 1, cpuResources(i64(800000), p, "0-1"), host, 2},
		{"a quota below the cpuset", 1, cpuResources(i64(200000), p, "0-7"), host, 2},
		{"a malformed cpuset alone", 2, cpuResources(nil, nil, "a-b"), host, 2},
		{"a malformed cpuset beside a quota", 1, cpuResources(i64(300000), p, "a-b"), host, 3},
		{"a quota above the host", 1, cpuResources(i64(6400000), p, ""), 8, 8},
		{"no host cap", 1, cpuResources(i64(6400000), p, ""), 0, 64},
		{"a zero default", 0, nil, host, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, monitorVCPUs(tc.def, tc.resources, tc.host))
		})
	}
}

func TestCpusetCount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		list string
		want uint
	}{
		{"0", 1},
		{"0-3,6", 5},
		{" 0 , 2 ", 2},
		{"0-3,2", 4},
		{"4-4", 1},
	} {
		got, err := cpusetCount(tc.list)
		require.NoError(t, err, tc.list)
		assert.Equal(t, tc.want, got, tc.list)
	}
	for _, list := range []string{"", ",", "x", "3-1", "0-", "-1", "0-4294967295"} {
		_, err := cpusetCount(list)
		assert.Error(t, err, list)
	}
}

// The quota reaches the monitor only for a container boot. A unikernel given
// the same limit keeps the monitor default.
func TestBuildMonitorSpecVCPUs(t *testing.T) {
	t.Parallel()
	limit := cpuResources(i64(200000), u64(100000), "")

	u, rootfsParams := newSpecUnikontainer(t, t.TempDir())
	u.Spec.Linux.Resources = limit
	u.State.Annotations[annotBootKernel] = "/opt/urunc/boot/bzImage"
	u.State.Annotations[annotBootInitrd] = "/opt/urunc/boot/container-initrd"
	require.True(t, u.isContainerBoot())
	got := u.buildMonitorSpec(rootfsParams, monitorResources{})
	assert.Equal(t, uint(min(2, runtime.NumCPU())), got.ExecArgs.VCPUs)

	u, rootfsParams = newSpecUnikontainer(t, t.TempDir())
	u.Spec.Linux.Resources = limit
	require.False(t, u.isContainerBoot())
	got = u.buildMonitorSpec(rootfsParams, monitorResources{})
	assert.Equal(t, u.UruncCfg.Monitors["qemu"].DefaultVCPUs, got.ExecArgs.VCPUs)
}
