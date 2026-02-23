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

package urunce2etesting

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

func pingUnikernel(ipAddress string) error {
	pinger, err := probing.NewPinger(ipAddress)
	if err != nil {
		return fmt.Errorf("failed to create Pinger: %v", err)
	}
	pinger.Count = 3
	pinger.Timeout = 5 * time.Second
	err = pinger.Run()
	if err != nil {
		return fmt.Errorf("failed to ping %s: %v", ipAddress, err)
	}
	if pinger.PacketsRecv != pinger.PacketsSent {
		return fmt.Errorf("packets received (%d) not equal to packets sent (%d)", pinger.PacketsRecv, pinger.PacketsSent)
	}
	if pinger.PacketsSent == 0 {
		return fmt.Errorf("no packets were sent")
	}
	return nil
}

func compareNS(cntr string, defNS string, specPath string) error {
	if specPath == "" {
		if cntr == defNS {
			return fmt.Errorf("Unikernel's namespace is the default")
		}
	} else {
		nsLink, err := os.Readlink(specPath)
		if err != nil {
			return err
		}
		if cntr != nsLink {
			return fmt.Errorf("Unikernel's namespace differs from spec's namespace")
		}
	}

	return nil
}

func getProcNS(proc string) (map[string]string, error) {
	procPath := filepath.Join("/proc", proc, "ns")
	ns := make(map[string]string)
	cgroupPath := filepath.Join(procPath, "cgroup")
	var err error
	ns["cgroup"], err = os.Readlink(cgroupPath)
	if err != nil {
		return nil, err
	}
	ipcPath := filepath.Join(procPath, "ipc")
	ns["ipc"], err = os.Readlink(ipcPath)
	if err != nil {
		return nil, err
	}
	mntPath := filepath.Join(procPath, "mnt")
	ns["mnt"], err = os.Readlink(mntPath)
	if err != nil {
		return nil, err
	}
	netPath := filepath.Join(procPath, "net")
	ns["net"], err = os.Readlink(netPath)
	if err != nil {
		return nil, err
	}
	pidPath := filepath.Join(procPath, "pid")
	ns["pid"], err = os.Readlink(pidPath)
	if err != nil {
		return nil, err
	}
	userPath := filepath.Join(procPath, "user")
	ns["user"], err = os.Readlink(userPath)
	if err != nil {
		return nil, err
	}
	utsPath := filepath.Join(procPath, "uts")
	ns["uts"], err = os.Readlink(utsPath)
	if err != nil {
		return nil, err
	}

	return ns, nil
}

func verifyNoStaleFiles(containerID string) error {
	// Check /run/containerd/runc/default/containerID directory does not exist
	dirPath := "/run/containerd/runc/default/" + containerID
	_, err := os.Stat(dirPath)
	if !os.IsNotExist(err) {
		return fmt.Errorf("root directory %s still exists", dirPath)
	}

	// Check /run/containerd/runc/k8s.io/containerID directory does not exist
	dirPath = "/run/containerd/runc/k8s.io/" + containerID
	_, err = os.Stat(dirPath)
	if !os.IsNotExist(err) {
		return fmt.Errorf("root directory %s still exists", dirPath)
	}

	// Check /run/containerd/io.containerd.runtime.v2.task/default/containerID directory does not exist
	dirPath = "/run/containerd/io.containerd.runtime.v2.task/default/" + containerID
	_, err = os.Stat(dirPath)
	if !os.IsNotExist(err) {
		return fmt.Errorf("bundle directory %s still exists", dirPath)
	}

	// Check /run/containerd/io.containerd.runtime.v2.task/k8s.io/containerID directory does not exist
	dirPath = "/run/containerd/io.containerd.runtime.v2.task/k8s.io/" + containerID
	_, err = os.Stat(dirPath)
	if !os.IsNotExist(err) {
		return fmt.Errorf("bundle directory %s still exists", dirPath)
	}

	return nil
}

func getAndCheckUGid(line string) (int, error) {
	vals := strings.Split(line, ":")
	ids := strings.Split(strings.TrimSpace(vals[1]), "\t")
	if len(ids) != 4 {
		return 0, fmt.Errorf("Invalid format of line. Expecting 4 values, got %d", len(ids))
	}
	if (ids[0] != ids[1]) || (ids[1] != ids[2]) || (ids[0] != ids[3]) {
		return 0, fmt.Errorf("ids in line do not match")
	}

	return strconv.Atoi(ids[0])
}

func findLineInFile(filePath string, pattern string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("Failed to open %s: %v", filePath, err)
	}

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, pattern) {
			return line, nil
		}
	}

	return "", fmt.Errorf("Pattern %s was not found in any line of %s", pattern, filePath)
}
