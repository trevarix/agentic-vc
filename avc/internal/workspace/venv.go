// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ensurePythonVenv creates a venv at <workspacePath>/.venv if absent.
// Returns the venv directory path.
func ensurePythonVenv(workspacePath string) (string, error) {
	venvPath := filepath.Join(workspacePath, ".venv")
	if _, err := os.Stat(venvPath); err == nil {
		return venvPath, nil
	}

	python := "python3"
	if _, err := exec.LookPath("python3"); err != nil {
		python = "python"
	}

	out, err := exec.Command(python, "-m", "venv", venvPath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("create venv: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return venvPath, nil
}

// pipBin returns the absolute path to the pip executable inside the venv.
func pipBin(venvPath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvPath, "Scripts", "pip.exe")
	}
	return filepath.Join(venvPath, "bin", "pip")
}

// venvBinDir returns the directory containing executables for the venv.
func venvBinDir(venvPath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvPath, "Scripts")
	}
	return filepath.Join(venvPath, "bin")
}

// replacePipWithVenv rewrites the first token of command to use the venv pip binary.
func replacePipWithVenv(venvPath, command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return command
	}
	fields[0] = pipBin(venvPath)
	return strings.Join(fields, " ")
}
