// Copyright (c) 2023-2025, Nubificus LTD
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

package metrics

type TimestampID uint

type TimestampMeta struct {
	ID       TimestampID
	LegacyID string // old ts names
	Name     string
	Order    int
}

const (
	TS00 TimestampID = iota
	TS01
	TS02
	TS03
	TS04
	TS05
	TS06
	TS07
	TS08
	TS09
	TS10
	TS11
	TS12
	TS13
	TS14
	TS15
	TS16
	TS17
	TS18
	TimestampCount
)

// Timestamps defines the fixed, stable ordering of all urunc runtime timestamps.
// Naming convention:
//   CR.* = create phase
//   ST.* = start phase
//   RX.* = reexec phase
// LegacyID (TSxx) is preserved for backward compatibility with existing tooling.
// The Order field determines the execution sequence and must remain stable.

var Timestamps = []TimestampMeta{
	{TS00, "TS00", "CR.invoked", 0},
	{TS01, "TS01", "CR.unikontainer_created", 1},
	{TS02, "TS02", "CR.initial_setup", 2},
	{TS03, "TS03", "CR.start_reexec", 3},
	{TS04, "TS04", "RX.create_invoked", 4},
	{TS05, "TS05", "RX.close_pipes_and_setup_base", 5},
	{TS06, "TS06", "CR.received_pids", 6},
	{TS07, "TS07", "CR.hooks_executed", 7},
	{TS08, "TS08", "CR.sent_ack", 8},
	{TS09, "TS09", "RX.received_ack", 9},
	{TS10, "TS10", "CR.terminated", 10},
	{TS11, "TS11", "ST.invoked", 11},
	{TS12, "TS12", "ST.unikontainer_created", 12},
	{TS13, "TS13", "ST.sent_start_msg", 13},
	{TS14, "TS14", "RX.received_start_msg", 14},
	{TS15, "TS15", "RX.joined_netns", 15},
	{TS16, "TS16", "RX.network_setup_completed", 16},
	{TS17, "TS17", "RX.disk_setup_completed", 17},
	{TS18, "TS18", "RX.execve_hypervisor", 18},
}
