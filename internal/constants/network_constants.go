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

// Package constants provides network-related constants for the urunc runtime.
package constants

const (
	// StaticNetworkTapIP is the IP address of the static network TAP device.
	StaticNetworkTapIP       = "172.16.1.1"
	StaticNetworkUnikernelIP = "172.16.1.2"
	// TODO: Experiment with DynamicNetworkTapIP starting from 172.16.X.1
	// DynamicNetworkTapIP is the IP address of the dynamic network TAP device.
	DynamicNetworkTapIP  = "172.16.X.2"
	QueueProxyRedirectIP = "172.16.1.2"
)
