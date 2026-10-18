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

// Package urunit tests the guest side of a generic container boot against
// the host side of urunc. The guest side is urunit, a static C binary from
// https://github.com/NOFireAI/urunit, at the commit
// packaging/container-initrd/urunit.pin names. It runs as the early init
// (/init, /vz-init), as the supervisor (PID 1) of the container application
// and as the exec agent.
//
// The tests drive the binary with the pkg/agentproto client, so a change to
// the protocol on either side shows up here. They run the binary URUNIT_BIN
// names and skip when it is unset:
//
//	make test_urunit
//
// builds the pinned binary and runs them, in a container on hosts other than
// Linux. Set URUNIT_SRC to test a local urunit checkout.
//
// Tests that switch users or check the owner of a pty need root. They skip
// when the tests run as another user.
package urunit
