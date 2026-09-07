// Copyright (c) 2023-2026, Nubificus LTD
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package unikontainers

import (
	"os"
	"path/filepath"
	"testing"
)

// LoadSpec + GetUnikernelConfig are now platform-neutral (untagged), so the
// darwin runner parses bundles through the same code as the Linux engine.
// This exercises that shared path with a bundle whose config.json carries
// plain com.urunc.unikernel.* spec annotations.
func TestSharedBundleParsing(t *testing.T) {
	bundle := t.TempDir()
	config := `{
	  "ociVersion": "1.0.2",
	  "root": {"path": "rootfs"},
	  "process": {"args": ["/unikernel", "hello"]},
	  "annotations": {
	    "com.urunc.unikernel.unikernelType": "linux",
	    "com.urunc.unikernel.hypervisor": "qemu",
	    "com.urunc.unikernel.binary": "/.boot/kernel",
	    "com.urunc.unikernel.cmdline": "console=ttyAMA0"
	  }
	}`
	if err := os.WriteFile(filepath.Join(bundle, "config.json"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	spec, err := LoadSpec(bundle)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if len(spec.Process.Args) != 2 || spec.Process.Args[0] != "/unikernel" {
		t.Errorf("process args = %v", spec.Process.Args)
	}

	cfg, err := GetUnikernelConfig(bundle, spec)
	if err != nil {
		t.Fatalf("GetUnikernelConfig: %v", err)
	}
	if cfg.UnikernelType != "linux" {
		t.Errorf("UnikernelType = %q, want linux", cfg.UnikernelType)
	}
	if cfg.Hypervisor != "qemu" {
		t.Errorf("Hypervisor = %q, want qemu", cfg.Hypervisor)
	}
	if cfg.UnikernelBinary != "/.boot/kernel" {
		t.Errorf("UnikernelBinary = %q", cfg.UnikernelBinary)
	}
}
