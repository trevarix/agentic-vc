// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package workspace

import (
	"path/filepath"
	"strings"
)

type commandClass int

const (
	classBlocked    commandClass = iota // system-level installs, privilege escalation
	classPipInstall                     // pip/pip3 install → redirect to workspace venv
	classPython                         // python/pytest/uv → activate against workspace venv
	classNode                           // npm/yarn/pnpm/npx
	classGo                             // go build/test/run/mod
	classGeneric                        // everything else — run as-is
)

// classify returns the command class based on the first token and flag scan.
func classify(command string) commandClass {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return classGeneric
	}

	first := strings.ToLower(filepath.Base(fields[0]))

	// Privilege escalation — always blocked.
	if first == "sudo" || first == "su" {
		return classBlocked
	}

	// System package managers — blocked on install subcommand.
	switch first {
	case "brew", "apt", "apt-get", "apk", "dnf", "yum", "choco", "winget", "snap":
		if len(fields) > 1 && fields[1] == "install" {
			return classBlocked
		}
	}

	// pip/pip3 — check for --user before routing to classPipInstall.
	if first == "pip" || first == "pip3" {
		if isBlockedGlobalFlag(fields) {
			return classBlocked
		}
		if len(fields) > 1 && fields[1] == "install" {
			return classPipInstall
		}
		return classPython
	}

	// npm — check for -g / --global before routing to classNode.
	if first == "npm" {
		if isBlockedGlobalFlag(fields) {
			return classBlocked
		}
		return classNode
	}

	switch first {
	case "python", "python3", "pytest", "py.test", "uv":
		return classPython
	case "yarn", "pnpm", "npx":
		return classNode
	case "go":
		return classGo
	default:
		return classGeneric
	}
}

// isBlockedGlobalFlag reports whether any token in fields is a flag that would
// cause writes outside the workspace (--user, -g, --global, --system).
func isBlockedGlobalFlag(fields []string) bool {
	for _, f := range fields[1:] {
		switch f {
		case "--user", "-g", "--global", "--system":
			return true
		}
	}
	return false
}

// blockedMessage returns a human-readable error explaining why a command was blocked.
func blockedMessage(command string) string {
	fields := strings.Fields(command)
	first := ""
	if len(fields) > 0 {
		first = strings.ToLower(filepath.Base(fields[0]))
	}

	switch first {
	case "sudo", "su":
		return "blocked: privilege escalation is not allowed in agent workspaces. Run the command without sudo."
	case "brew":
		return "blocked: brew install modifies the host system. Install project dependencies via pip, npm, or go instead."
	case "apt", "apt-get":
		return "blocked: apt install modifies system packages. Install project dependencies via pip, npm, or go instead."
	case "apk", "dnf", "yum":
		return "blocked: system package manager is not allowed in agent workspaces."
	case "choco", "winget":
		return "blocked: system package manager is not allowed in agent workspaces."
	case "pip", "pip3":
		return "blocked: pip install --user writes to your global site-packages. Use pip install (without --user) — a workspace venv is created automatically."
	case "npm":
		return "blocked: npm install -g / --global writes to the global node_modules. Use npm install (without -g) — packages install into the workspace automatically."
	}
	return "blocked: this command is not allowed in agent workspaces."
}
