//go:build linux

// Copyright (c) 2023-2026, Nubificus LTD
// SPDX-License-Identifier: Apache-2.0

package unikontainers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func TestStageContainerBootFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	kernel := filepath.Join(dir, "bzImage")
	bootInitrd := filepath.Join(dir, "container-initrd")
	resolver := filepath.Join(dir, "resolv.conf")
	if err := os.WriteFile(kernel, []byte("kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bootInitrd, []byte("base-initrd"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolver, []byte("nameserver 192.0.2.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	monRoot := filepath.Join(dir, "monitor")
	b := blockRootfs{
		monRootfs:      monRoot,
		kernelPath:     "/boot/vmlinuz",
		bootKernelHost: kernel,
		bootInitrdHost: bootInitrd,
		containerCmd:   []string{"/bin/sh", "-c", "echo hello world"},
		containerEnv:   []string{"PATH=/usr/bin:/bin", "VALUE=hello world"},
		mounts: []specs.Mount{{
			Type: "bind", Source: resolver, Destination: "/etc/resolv.conf",
		}},
	}
	if err := b.stageContainerBootFiles(); err != nil {
		t.Fatalf("stageContainerBootFiles: %v", err)
	}

	gotKernel, err := os.ReadFile(filepath.Join(monRoot, containerRootfsMountPath, "boot/vmlinuz"))
	if err != nil || string(gotKernel) != "kernel" {
		t.Fatalf("staged kernel = %q, %v", gotKernel, err)
	}
	gotInitrd, err := os.ReadFile(filepath.Join(monRoot, containerRootfsMountPath, containerBootInitrdPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"base-initrd", "/bin/sh", "echo hello world", "VALUE=hello world", "nameserver 192.0.2.1"} {
		if !strings.Contains(string(gotInitrd), want) {
			t.Fatalf("staged initrd does not contain %q", want)
		}
	}
}

func TestContainerBootAnnotationsMustBePaired(t *testing.T) {
	t.Parallel()
	b := blockRootfs{bootKernelHost: "/kernel"}
	if err := b.stageContainerBootFiles(); err == nil || !strings.Contains(err.Error(), annotBootInitrd) {
		t.Fatalf("expected paired-annotation error, got %v", err)
	}
}
