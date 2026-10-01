#!/bin/bash
# Copyright (c) 2023-2026, Nubificus LTD
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

echo "Setting up urunc development environment..."

git config --global --add safe.directory /workspace

go env -w GOPROXY=https://proxy.golang.org,direct

go mod download

echo "Checking installed tools..."
qemu-system-x86_64 --version || true
firecracker --version || true
cloud-hypervisor --version || true
go version

echo "Setup complete. Run 'make unittest' to verify."
