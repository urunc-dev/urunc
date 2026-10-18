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
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/urunc-dev/urunc/pkg/agentproto"
)

// fakeRoots points sysRoot and devRoot at fresh temporary directories for
// the duration of the test and returns them.
func fakeRoots(t *testing.T) (sys, dev string) {
	t.Helper()
	oldSys, oldDev := sysRoot, devRoot
	t.Cleanup(func() { sysRoot, devRoot = oldSys, oldDev })
	sysRoot, devRoot = t.TempDir(), t.TempDir()
	return sysRoot, devRoot
}

// addPort creates /sys/class/virtio-ports/<port>/name holding name and the
// matching /dev/<port> node under the fake roots.
func addPort(t *testing.T, sys, dev, port, name string) {
	t.Helper()
	dir := filepath.Join(sys, "class", "virtio-ports", port)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "name"), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dev, port), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindVirtioPort(t *testing.T) {
	t.Run("sysfs name matches", func(t *testing.T) {
		sys, dev := fakeRoots(t)
		addPort(t, sys, dev, "vport0p0", "org.qemu.guest_agent.0")
		addPort(t, sys, dev, "vport1p1", agentproto.VirtioPortName)
		want := filepath.Join(dev, "vport1p1")
		if got := findVirtioPort(); got != want {
			t.Fatalf("findVirtioPort() = %q, want %q", got, want)
		}
	})

	t.Run("sysfs name differs", func(t *testing.T) {
		sys, dev := fakeRoots(t)
		addPort(t, sys, dev, "vport1p1", "io.urunc.other.0")
		if got := findVirtioPort(); got != "" {
			t.Fatalf("findVirtioPort() = %q, want \"\"", got)
		}
	})

	t.Run("no virtio-ports class", func(t *testing.T) {
		fakeRoots(t)
		if got := findVirtioPort(); got != "" {
			t.Fatalf("findVirtioPort() = %q, want \"\"", got)
		}
	})

	t.Run("matching name without device node", func(t *testing.T) {
		sys, dev := fakeRoots(t)
		addPort(t, sys, dev, "vport1p1", agentproto.VirtioPortName)
		if err := os.Remove(filepath.Join(dev, "vport1p1")); err != nil {
			t.Fatal(err)
		}
		if got := findVirtioPort(); got != "" {
			t.Fatalf("findVirtioPort() = %q, want \"\"", got)
		}
	})

	t.Run("udev symlink wins", func(t *testing.T) {
		sys, dev := fakeRoots(t)
		addPort(t, sys, dev, "vport1p1", agentproto.VirtioPortName)
		links := filepath.Join(dev, "virtio-ports")
		if err := os.MkdirAll(links, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(links, agentproto.VirtioPortName)
		if err := os.Symlink("../vport1p1", link); err != nil {
			t.Fatal(err)
		}
		if got := findVirtioPort(); got != link {
			t.Fatalf("findVirtioPort() = %q, want %q", got, link)
		}
	})
}

// connReader serves one scripted byte string per connection and ends it
// with EOF, as a virtio-serial port does on host disconnect. After the
// last connection it fails with EIO.
type connReader struct {
	conns [][]byte
	cur   int
}

func (c *connReader) Read(p []byte) (int, error) {
	if c.cur >= len(c.conns) {
		return 0, syscall.EIO
	}
	if len(c.conns[c.cur]) == 0 {
		c.cur++
		return 0, io.EOF
	}
	n := copy(p, c.conns[c.cur])
	c.conns[c.cur] = c.conns[c.cur][n:]
	return n, nil
}

func TestServeConnsReconnect(t *testing.T) {
	// A client that dies after four bytes of a frame header.
	half := []byte{agentproto.TypeOpen, 0, 0, 0}
	var full bytes.Buffer
	if err := agentproto.WriteFrame(&full, agentproto.TypeOpen, 7, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	rd := &connReader{conns: [][]byte{nil, half, full.Bytes()}}
	var out bytes.Buffer
	err := serveConns(struct {
		io.Reader
		io.Writer
	}{rd, &out}, time.Millisecond)
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("serveConns() = %v, want EIO", err)
	}
	f, err := agentproto.ReadFrame(&out)
	if err != nil {
		t.Fatalf("no reply frame: %v", err)
	}
	if f.Type != agentproto.TypeError || f.Stream != 7 {
		t.Fatalf("reply = type %d stream %d, want error on stream 7", f.Type, f.Stream)
	}
	if out.Len() != 0 {
		t.Fatalf("%d unexpected bytes written", out.Len())
	}
}

func TestChooseTransport(t *testing.T) {
	tests := []struct {
		env  string
		want transportPlan
	}{
		{env: "", want: transportPlan{vsock: true, port: true}},
		{env: "vsock", want: transportPlan{vsock: true}},
		{env: "virtio-port", want: transportPlan{port: true}},
		{env: "bogus", want: transportPlan{vsock: true, port: true}},
	}
	for _, tc := range tests {
		if got := chooseTransport(tc.env); got != tc.want {
			t.Errorf("chooseTransport(%q) = %+v, want %+v", tc.env, got, tc.want)
		}
	}
}
