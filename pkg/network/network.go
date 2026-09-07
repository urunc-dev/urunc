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

package network

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

var netlog = logrus.WithField("subsystem", "network")

type UnikernelNetworkInfo struct {
	TapDevice string
	EthDevice Interface
}

type Manager interface {
	NetworkSetup(uid uint32, gid uint32) (*UnikernelNetworkInfo, error)
}

type Interface struct {
	IP             string
	DefaultGateway string
	Mask           string
	Interface      string
	MAC            string
	MTU            int
}

func NewNetworkManager(networkType string) (Manager, error) {
	switch networkType {
	case "static":
		return &StaticNetwork{}, nil
	case "dynamic":
		return &DynamicNetwork{}, nil
	default:
		return nil, fmt.Errorf("network manager %s not supported", networkType)

	}
}
