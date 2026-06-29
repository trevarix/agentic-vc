// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build windows

package workspace

import (
	"fmt"
	"os/exec"
)

func setupProcessGroup(cmd *exec.Cmd) {
	// No-op on Windows — tree kill is handled by taskkill in killProcessTree.
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// taskkill /F /T kills the process and all its children atomically.
	exec.Command("taskkill", "/F", "/T", "/PID", //nolint:errcheck
		fmt.Sprintf("%d", cmd.Process.Pid)).Run()
}
