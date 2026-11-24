// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Top-level keys that hold MCP server entries. Most hosts use mcpServers;
// VS Code's .vscode/mcp.json uses servers.
const (
	keyMCPServers = "mcpServers"
	keyVSCode     = "servers"
)

// serverArgs returns the arguments every generated config passes to avc,
// followed by any search roots.
func serverArgs(roots ...string) []string {
	return append([]string{"mcp", "serve", "--tools", defaultToolTier}, roots...)
}

// jsonServer describes one server entry to merge into a JSON MCP config.
type jsonServer struct {
	path       string         // config file
	serversKey string         // keyMCPServers or keyVSCode
	name       string         // entry name, e.g. "avc"
	entry      map[string]any // the entry to write
	// matches reports whether an existing entry already does the job, so the
	// file is left alone. It deliberately ignores fields a user may tune (the
	// tool tier in args), so re-running init never undoes that.
	matches func(existing map[string]any) bool
}

// commandMatches is the usual jsonServer.matches: same command, and the same
// env block when env is set.
func commandMatches(command string, env map[string]any) func(map[string]any) bool {
	return func(existing map[string]any) bool {
		if existing["command"] != command {
			return false
		}
		if env == nil {
			return true
		}
		existingEnv, ok := existing["env"].(map[string]any)
		return ok && reflect.DeepEqual(existingEnv, env)
	}
}

// mergeJSONServer merges s into its JSON config file, preserving every other
// entry. Returns "created", "updated", or "skipped:already configured".
func mergeJSONServer(s jsonServer) (string, error) {
	root, existed, err := readJSONObject(s.path)
	if err != nil {
		return "", err
	}

	servers, ok := root[s.serversKey].(map[string]any)
	if !ok {
		servers = map[string]any{}
		root[s.serversKey] = servers
	}
	if existing, ok := servers[s.name].(map[string]any); ok && s.matches(existing) {
		return skipAlreadyConfigured, nil
	}

	servers[s.name] = s.entry
	if err := writeJSON(s.path, root); err != nil {
		return "", err
	}
	if existed {
		return actionUpdated, nil
	}
	return actionCreated, nil
}

// readJSONObject reads a JSON object from path. A missing file yields an empty
// object and existed=false. A file that is not plain JSON — VS Code allows
// comments in its config files — is an error naming the file, so the caller
// can tell the user to add the entry by hand rather than rewrite their file.
func readJSONObject(path string) (root map[string]any, existed bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, true, nil
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, true, fmt.Errorf("parse %s: %w", path, err)
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, true, nil
}

// writeJSON writes root to path as indented JSON with a trailing newline.
func writeJSON(path string, root map[string]any) error {
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return writeFileMkdir(path, string(append(out, '\n')))
}

// warnStaleGlobalEntry warns when serverKey is still registered in a global
// config file (from an older AVC version that wrote global configs), which
// would register the server twice.
func warnStaleGlobalEntry(globalPath, serverKey string, r *WriteResult) {
	root, existed, err := readJSONObject(globalPath)
	if err != nil || !existed {
		return
	}
	servers, ok := root[keyMCPServers].(map[string]any)
	if !ok {
		return
	}
	if _, exists := servers[serverKey]; exists {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"a global '%s' MCP entry also exists in %s — the server would be registered twice; remove that entry", serverKey, globalPath))
	}
}

// ─── Codex (TOML) ─────────────────────────────────────────────────────────────

// codexServerTable is the TOML table header for AVC's entry in a Codex config.
const codexServerTable = "[mcp_servers.avc]"

// codexConfig is the subset of a Codex config.toml AVC reads.
type codexConfig struct {
	MCPServers map[string]struct {
		Command string   `toml:"command"`
		Args    []string `toml:"args"`
	} `toml:"mcp_servers"`
}

// mergeCodexServer adds AVC's [mcp_servers.avc] table to a Codex config.toml.
//
// The file is edited as text, not decoded and re-encoded: a round trip
// through a TOML library would drop the user's comments and reorder their
// keys. Only AVC's own table is replaced or appended, and the result is
// parsed before it is written, so a mistake can never leave Codex with a
// config it cannot read.
func mergeCodexServer(path, command string) (string, error) {
	data, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	content := string(data)

	var cfg codexConfig
	if _, err := toml.Decode(content, &cfg); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	if s, ok := cfg.MCPServers["avc"]; ok && s.Command == command {
		return skipAlreadyConfigured, nil
	}

	table := codexTable(command)
	var updated string
	if start, end, ok := findTOMLTable(content, codexServerTable); ok {
		updated = content[:start] + table + content[end:]
	} else {
		sep := ""
		if content != "" {
			sep = "\n"
			if !strings.HasSuffix(content, "\n") {
				sep = "\n\n"
			}
		}
		updated = content + sep + table
	}

	if _, err := toml.Decode(updated, &codexConfig{}); err != nil {
		return "", fmt.Errorf("refusing to write %s: the result would not parse: %w", path, err)
	}
	if err := writeFileMkdir(path, updated); err != nil {
		return "", err
	}
	if existed {
		return actionUpdated, nil
	}
	return actionCreated, nil
}

// codexTable renders AVC's server table.
func codexTable(command string) string {
	args := serverArgs()
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = tomlString(a)
	}
	return codexServerTable + "\n" +
		"command = " + tomlString(command) + "\n" +
		"args = [" + strings.Join(quoted, ", ") + "]\n"
}

// tomlString quotes s as a TOML string. A literal string needs no escaping, so
// Windows paths stay readable; s containing a quote falls back to a basic one.
func tomlString(s string) string {
	if !strings.ContainsAny(s, "'\n") {
		return "'" + s + "'"
	}
	return strconv.Quote(s)
}

// findTOMLTable returns the byte range of the table with the given header:
// from the header line to the next table header or the end of the file.
func findTOMLTable(content, header string) (start, end int, ok bool) {
	offset := 0
	start = -1
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if start < 0 && trimmed == header {
			start = offset
		} else if start >= 0 && strings.HasPrefix(trimmed, "[") {
			return start, offset, true
		}
		offset += len(line)
	}
	if start < 0 {
		return 0, 0, false
	}
	return start, len(content), true
}

// windsurfRoots returns the search roots already in a Windsurf entry's args,
// plus projectRoot, deduplicated in order. Windsurf has one global server for
// every project, so each `avc init --skills windsurf` adds its project rather
// than replacing the last one.
func windsurfRoots(existing map[string]any, projectRoot string) []string {
	base := len(serverArgs())
	var roots []string
	seen := map[string]bool{}
	if args, ok := existing["args"].([]any); ok && len(args) > base {
		for _, a := range args[base:] {
			if s, ok := a.(string); ok && !seen[filepath.Clean(s)] {
				seen[filepath.Clean(s)] = true
				roots = append(roots, s)
			}
		}
	}
	if !seen[filepath.Clean(projectRoot)] {
		roots = append(roots, projectRoot)
	}
	return roots
}

// argsContain reports whether an entry's args include s.
func argsContain(entry map[string]any, s string) bool {
	args, _ := entry["args"].([]any)
	for _, a := range args {
		if str, ok := a.(string); ok && filepath.Clean(str) == filepath.Clean(s) {
			return true
		}
	}
	return false
}
