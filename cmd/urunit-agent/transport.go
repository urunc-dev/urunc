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

//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urunc-dev/urunc/pkg/agentproto"
	"golang.org/x/sys/unix"
)

// transportEnv names the environment variable that pins the agent to one
// transport. hull's vz-init sets it to "vsock".
const transportEnv = "URUNIT_AGENT_TRANSPORT"

// The values transportEnv accepts.
const (
	transportVsockOnly = "vsock"
	transportPortOnly  = "virtio-port"
)

// The sysfs and /dev roots findVirtioPort looks under. Tests point them at
// a temporary directory.
var (
	sysRoot = "/sys"
	devRoot = "/dev"
)

// virtioPortRetry is how long serveConns waits after a host disconnect
// before it serves the port again.
const virtioPortRetry = 100 * time.Millisecond

// transportPlan is the order the agent tries its transports in.
type transportPlan struct {
	vsock bool // serve on vsock first
	port  bool // probe the virtio-serial port, after vsock fails when vsock is set
}

// chooseTransport maps the value of transportEnv to a transportPlan. By
// default the agent serves on vsock and probes the virtio-serial port only
// when vsock fails, so a guest with vsock behaves as if the port did not
// exist. "vsock" disables the probe and "virtio-port" skips vsock. Any
// other value falls back to the default.
func chooseTransport(env string) transportPlan {
	switch env {
	case transportVsockOnly:
		return transportPlan{vsock: true}
	case transportPortOnly:
		return transportPlan{port: true}
	default:
		if env != "" {
			log.Printf("ignoring unknown %s=%q", transportEnv, env)
		}
		return transportPlan{vsock: true, port: true}
	}
}

// probeVirtioPort looks for the agent's virtio-serial port once. The agent
// may start before init mounts the special filesystems, so mount sysfs
// first when /sys/class is missing. Only the fallback path gets here, so a
// guest that serves on vsock never touches its mounts.
func probeVirtioPort() string {
	if !exists(filepath.Join(sysRoot, "class")) {
		if err := unix.Mount("sysfs", sysRoot, "sysfs", 0, ""); err != nil {
			log.Printf("mount %s: %v", sysRoot, err)
		}
	}
	return findVirtioPort()
}

// findVirtioPort locates the agent's virtio-serial port device. The
// /dev/virtio-ports/<name> symlink is created by udev, which minimal
// guests do not run, so fall back to resolving the name through sysfs
// (/sys/class/virtio-ports/vportNpM/name -> /dev/vportNpM). It returns ""
// when no port carries agentproto.VirtioPortName.
func findVirtioPort() string {
	if link := filepath.Join(devRoot, "virtio-ports", agentproto.VirtioPortName); exists(link) {
		return link
	}
	classDir := filepath.Join(sysRoot, "class", "virtio-ports")
	entries, err := os.ReadDir(classDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name, err := os.ReadFile(filepath.Join(classDir, e.Name(), "name"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(name)) != agentproto.VirtioPortName {
			continue
		}
		if dev := filepath.Join(devRoot, e.Name()); exists(dev) {
			return dev
		}
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// serveVirtioPort serves sessions over the virtio-serial port dev. The port
// is a single byte stream shared with one host peer at a time, so all
// sessions multiplex on it. A read returns EOF while no host peer is
// connected and after a peer disconnects. Each EOF ends the current
// connection, which kills the sessions it opened and drops any
// half-read frame, and the next connection starts on the same descriptor
// with a fresh parser, so there is no close/reopen gap for frames sent
// right after a host connect. It returns only when the port cannot be
// opened or a read fails with a real error.
func serveVirtioPort(dev string) error {
	f, err := os.OpenFile(dev, os.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", dev, err)
	}
	defer f.Close()
	log.Printf("serving on virtio port %s", dev)
	if err := serveConns(f, virtioPortRetry); err != nil {
		return fmt.Errorf("virtio port %s: %w", dev, err)
	}
	return fmt.Errorf("virtio port %s closed", dev)
}

// serveConns runs serveConn on rw once per host connection. An EOF from rw
// is a host disconnect: serveConn tears the peer's sessions down, and after
// retry the next connection gets a new frame parser. A read error other
// than EOF is returned.
func serveConns(rw io.ReadWriter, retry time.Duration) error {
	pr := &portReader{r: rw}
	for {
		serveConn(struct {
			io.Reader
			io.Writer
		}{pr, rw})
		if pr.err != nil {
			return pr.err
		}
		time.Sleep(retry)
	}
}

// portReader passes reads through and remembers the first error that is
// not EOF.
type portReader struct {
	r   io.Reader
	err error
}

func (p *portReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if err != nil && !errors.Is(err, io.EOF) {
		p.err = err
	}
	return n, err
}
