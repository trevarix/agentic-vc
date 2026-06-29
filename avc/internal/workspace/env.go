// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var sensitiveVarPrefixes = []string{
	"AWS_", "GITHUB_", "SSH_", "GPG_", "SECRET_",
	"TOKEN_", "API_KEY", "DATABASE_URL",
}

var pathAllowlist = []string{
	"python", "python3", "pip", "pip3",
	"node", "npm", "npx", "yarn", "pnpm",
	"go", "cargo", "ruby", "gem",
	"java", "mvn", "gradle",
	"make", "cmake",
	"sh", "bash", "env",
	"ls", "cat", "echo", "grep", "find",
	"cp", "mv", "rm", "mkdir", "touch",
}

// buildEnv returns a scrubbed environment for the subprocess.
// HOME is redirected to a temp dir inside the workspace.
// PATH is restricted to the approved runtime allowlist.
// Sensitive variables matching known patterns are stripped.
func buildEnv(workspacePath string) []string {
	homeDir := filepath.Join(workspacePath, ".avc-home")
	os.MkdirAll(homeDir, 0755) //nolint:errcheck

	filteredPath := buildFilteredPath()

	var result []string
	pathSet := false
	homeSet := false

	for _, e := range os.Environ() {
		idx := strings.IndexByte(e, '=')
		if idx < 0 {
			continue
		}
		key := e[:idx]

		if isSensitiveVar(key) {
			continue
		}

		keyUpper := strings.ToUpper(key)

		if keyUpper == "PATH" {
			result = append(result, "PATH="+filteredPath)
			pathSet = true
			continue
		}
		if keyUpper == "HOME" || keyUpper == "USERPROFILE" {
			result = append(result, key+"="+homeDir)
			homeSet = true
			continue
		}

		result = append(result, e)
	}

	if !pathSet {
		result = append(result, "PATH="+filteredPath)
	}
	if !homeSet {
		result = append(result, "HOME="+homeDir)
	}

	return result
}

// prependToPath returns env with dir prepended to the PATH entry.
func prependToPath(env []string, dir string) []string {
	result := make([]string, len(env))
	copy(result, env)
	for i, e := range result {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			result[i] = "PATH=" + dir + string(os.PathListSeparator) + e[5:]
			return result
		}
	}
	return append(result, "PATH="+dir)
}

// buildFilteredPath scans the system PATH for each allowlist executable and
// returns a PATH containing only the directories where those executables live.
func buildFilteredPath() string {
	seen := make(map[string]bool)
	var dirs []string
	for _, name := range pathAllowlist {
		if p, err := exec.LookPath(name); err == nil {
			d := filepath.Dir(p)
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// isSensitiveVar reports whether a variable name matches a known sensitive pattern.
func isSensitiveVar(key string) bool {
	upper := strings.ToUpper(key)
	for _, prefix := range sensitiveVarPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	// Exact matches.
	switch upper {
	case "DATABASE_URL", "REDIS_URL", "MONGO_URL":
		return true
	}
	// Suffix patterns.
	if strings.HasSuffix(upper, "_PASSWORD") || strings.HasSuffix(upper, "_SECRET") ||
		strings.HasSuffix(upper, "_TOKEN") || strings.HasSuffix(upper, "_KEY") {
		return true
	}
	return false
}
