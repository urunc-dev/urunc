// Copyright (c) 2023-2026, Nubificus LTD
// SPDX-License-Identifier: Apache-2.0

package unikernels

import (
	"strings"
	"testing"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestContainerBootUsesInjectedInitBeforeBlockRootfs(t *testing.T) {
	t.Parallel()

	l := &Linux{}
	err := l.Init(types.UnikernelParams{
		CmdLine:       []string{"/bin/sh", "-c", "printf container-boot-ok"},
		EnvVars:       []string{"TOKEN=not-on-kernel-command-line"},
		Monitor:       "hvi",
		Rootfs:        types.RootfsParams{Type: "block"},
		ContainerBoot: true,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	cmdline, err := l.CommandString()
	if err != nil {
		t.Fatalf("CommandString: %v", err)
	}
	for _, want := range []string{"root=/dev/vda", "rdinit=/init", "-- /bin/sh -c 'printf container-boot-ok'"} {
		if !strings.Contains(cmdline, want) {
			t.Fatalf("expected %q in %q", want, cmdline)
		}
	}
	if strings.Contains(cmdline, "TOKEN=") {
		t.Fatalf("container environment leaked onto kernel command line: %q", cmdline)
	}
}
