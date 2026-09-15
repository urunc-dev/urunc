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
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	probing "github.com/prometheus-community/pro-bing"
	"golang.org/x/sync/singleflight"
	"golang.org/x/sys/unix"
)

const (
	maxPullRetries = 5
	pullRetryDelay = 2 * time.Second
)

var (
	pullGroup        singleflight.Group
	imageTrackerLock sync.RWMutex
	// trackedImages maps tool -> image -> bool (true = pulled by this run, eligible for cleanup).
	trackedImages = make(map[ToolType]map[string]bool)
)

func withFileLock(domain string, fn func() error) error {
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("urunc-e2e-%s.lock", domain))
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fn()
	}
	defer f.Close()

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fn()
	}
	defer func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	}()

	return fn()
}

func imageExists(tool ToolType, image string) (bool, error) {
	var cmd string
	switch tool {
	case ToolCrictl:
		cmd = crictlName + " images -q " + image
	case ToolNerdctl:
		cmd = nerdctlName + " images -q " + image
	case ToolDocker:
		cmd = dockerName + " images -q " + image
	default:
		cmd = ctrName + " images list -q \"name==" + image + "\""
	}

	output, errorOut, err := commonCmdExecStderr(cmd)
	if err != nil {
		return false, fmt.Errorf("%s check failed: %s (%w)", tool, errorOut, err)
	}

	return strings.TrimSpace(output) != "", nil
}

func ensureImage(tool ToolType, image string) error {
	imageTrackerLock.RLock()
	if _, ok := trackedImages[tool][image]; ok {
		imageTrackerLock.RUnlock()
		return nil
	}
	imageTrackerLock.RUnlock()

	key := string(tool) + ":" + image
	_, err, _ := pullGroup.Do(key, func() (interface{}, error) {
		imageTrackerLock.RLock()
		if _, ok := trackedImages[tool][image]; ok {
			imageTrackerLock.RUnlock()
			return nil, nil
		}
		imageTrackerLock.RUnlock()

		exists, err := imageExists(tool, image)
		if err != nil {
			return nil, fmt.Errorf("failed to check image presence for %s: %w", image, err)
		}

		if exists {
			imageTrackerLock.Lock()
			if trackedImages[tool] == nil {
				trackedImages[tool] = make(map[string]bool)
			}
			trackedImages[tool][image] = false
			imageTrackerLock.Unlock()
			return nil, nil
		}

		err = withFileLock(tool.LockDomain(), func() error {
			log.Printf("Pulling image for %s: %s", tool, image)
			return pullImageWithRetry(tool, image)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to pull %s: %w", image, err)
		}

		imageTrackerLock.Lock()
		if trackedImages[tool] == nil {
			trackedImages[tool] = make(map[string]bool)
		}
		trackedImages[tool][image] = true
		imageTrackerLock.Unlock()

		return nil, nil
	})

	return err
}

func ensureTestImages(tool testTool, tc containerTestArgs) error {
	if tc.Image != "" {
		if err := ensureImage(tool.ToolType(), tc.Image); err != nil {
			return err
		}
	}
	for _, side := range tc.SideContainers {
		if side != "" {
			if err := ensureImage(tool.ToolType(), side); err != nil {
				return err
			}
		}
	}
	return nil
}

func cleanupImages(tool ToolType) {
	if os.Getenv("URUNC_E2E_KEEP_IMAGES") == "1" || strings.ToLower(os.Getenv("URUNC_E2E_KEEP_IMAGES")) == "true" {
		log.Printf("URUNC_E2E_KEEP_IMAGES is set; skipping image cleanup for %s", tool)
		return
	}

	imageTrackerLock.Lock()
	toolImages := trackedImages[tool]
	var toRemove []string
	for img, pulledByUs := range toolImages {
		if pulledByUs {
			toRemove = append(toRemove, img)
		}
	}
	delete(trackedImages, tool)
	imageTrackerLock.Unlock()

	if len(toRemove) == 0 {
		return
	}

	_ = withFileLock(tool.LockDomain(), func() error {
		for _, image := range toRemove {
			log.Printf("Removing image for %s: %s", tool, image)
			if err := removeImageForTest(tool, image); err != nil {
				log.Printf("Warning: failed to remove %s: %v", image, err)
			}
		}
		return nil
	})
}

func pullImageWithRetry(testFunc ToolType, image string) error {
	var err error
	for i := 0; i < maxPullRetries; i++ {
		err = pullImageForTest(testFunc, image)
		if err == nil {
			return nil
		}

		fmt.Printf("Attempt %d/%d failed to pull %s: %v. Retrying in %v...\n", i+1, maxPullRetries, image, err, pullRetryDelay)
		time.Sleep(pullRetryDelay)
	}
	return fmt.Errorf("failed to pull %s after %d attempts: %w", image, maxPullRetries, err)
}

func pullImageForTest(testFunc ToolType, image string) error {
	switch testFunc {
	case ToolCrictl:
		cmd := crictlName + " pull " + image
		output, err := commonCmdExec(cmd)
		if err != nil {
			return fmt.Errorf("%s -- %v", output, err)
		}
		return nil
	case ToolNerdctl:
		return commonPull(nerdctlName, image)
	case ToolDocker:
		return commonPull(dockerName, image)
	default:
		return commonPull(ctrName, image)
	}
}

func removeImageForTest(testFunc ToolType, image string) error {
	switch testFunc {
	case ToolCrictl:
		cmd := crictlName + " rmi " + image
		output, err := commonCmdExec(cmd)
		if err != nil {
			return fmt.Errorf("%s -- %v", output, err)
		}
		return nil
	case ToolNerdctl:
		return commonRmImage(nerdctlName, image)
	case ToolDocker:
		return commonRmImage(dockerName, image)
	default:
		return commonRmImage(ctrName, image)
	}
}

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
