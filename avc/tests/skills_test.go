// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Tests for avc init --skills: project-level vs global MCP config placement,
// merge/idempotency behavior, and stale-global-entry warnings.
package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/skills"
)

// containsAll reports whether s contains every substring.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// fakeHome redirects the home directory to an isolated temp dir so global
// config writes never touch the real one.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func readMCPServers(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	servers, ok := root["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no mcpServers object", path)
	}
	return servers
}

func TestSkillsClaudeCodeProjectLevel(t *testing.T) {
	home := fakeHome(t)
	project := t.TempDir()

	r, err := skills.Write(project, skills.FrameworkClaudeCode, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	servers := readMCPServers(t, filepath.Join(project, ".mcp.json"))
	entry, ok := servers["avc"].(map[string]any)
	if !ok {
		t.Fatal(".mcp.json has no avc server entry")
	}
	if entry["command"] == "" {
		t.Error("avc entry has empty command")
	}
	args, _ := entry["args"].([]any)
	if len(args) != 4 || args[0] != "mcp" || args[1] != "serve" {
		t.Errorf("unexpected args: %v", args)
	}

	// Global config must not be created in project mode.
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Error("project-mode write created ~/.claude.json")
	}

	// Skill files and CLAUDE.md still written.
	if _, err := os.Stat(filepath.Join(project, "CLAUDE.md")); err != nil {
		t.Error("CLAUDE.md not written")
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "avc-snapshot", "SKILL.md")); err != nil {
		t.Error("skill files not written")
	}
	for _, w := range r.Warnings {
		t.Logf("warning: %s", w)
	}
}

func TestSkillsClaudeCodeGlobal(t *testing.T) {
	home := fakeHome(t)
	project := t.TempDir()

	if _, err := skills.Write(project, skills.FrameworkClaudeCode, true); err != nil {
		t.Fatalf("Write: %v", err)
	}

	servers := readMCPServers(t, filepath.Join(home, ".claude.json"))
	if _, ok := servers["avc"]; !ok {
		t.Fatal("~/.claude.json has no avc server entry")
	}
	if _, err := os.Stat(filepath.Join(project, ".mcp.json")); !os.IsNotExist(err) {
		t.Error("global-mode write created project .mcp.json")
	}
}

func TestSkillsCursorProjectLevel(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()

	if _, err := skills.Write(project, skills.FrameworkCursor, false); err != nil {
		t.Fatalf("Write: %v", err)
	}

	servers := readMCPServers(t, filepath.Join(project, ".cursor", "mcp.json"))
	if _, ok := servers["avc"]; !ok {
		t.Fatal(".cursor/mcp.json has no avc server entry")
	}
	if _, err := os.Stat(filepath.Join(project, ".cursor", "rules", "avc.mdc")); err != nil {
		t.Error("cursor rules file not written")
	}
}

func TestSkillsProjectConfigIdempotentAndMergePreserving(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()

	// Pre-existing server entry must survive the merge.
	pre := map[string]any{
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "other-tool"},
		},
	}
	data, _ := json.Marshal(pre)
	if err := os.WriteFile(filepath.Join(project, ".mcp.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := skills.Write(project, skills.FrameworkClaudeCode, false); err != nil {
		t.Fatalf("Write: %v", err)
	}
	servers := readMCPServers(t, filepath.Join(project, ".mcp.json"))
	if _, ok := servers["other"]; !ok {
		t.Error("merge dropped pre-existing server entry")
	}
	if _, ok := servers["avc"]; !ok {
		t.Error("avc entry not added")
	}

	// Second run must skip, not rewrite.
	r, err := skills.Write(project, skills.FrameworkClaudeCode, false)
	if err != nil {
		t.Fatalf("second Write: %v", err)
	}
	for _, a := range r.Actions {
		if a.Path == ".mcp.json" && a.Status != "skipped" {
			t.Errorf(".mcp.json second run: got status %q, want skipped", a.Status)
		}
	}
}

func TestSkillsWarnsOnStaleGlobalEntry(t *testing.T) {
	home := fakeHome(t)
	project := t.TempDir()

	stale := map[string]any{
		"mcpServers": map[string]any{"avc": map[string]any{"command": "avc"}},
	}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	r, err := skills.Write(project, skills.FrameworkClaudeCode, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	found := false
	for _, w := range r.Warnings {
		if containsAll(w, "global", ".claude.json") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected stale-global-entry warning, got: %v", r.Warnings)
	}
}

func TestSkillsGitignoreWarningForProjectMCP(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()

	if err := os.WriteFile(filepath.Join(project, ".gitignore"), []byte(".mcp.json\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := skills.Write(project, skills.FrameworkClaudeCode, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	found := false
	for _, w := range r.Warnings {
		if containsAll(w, ".mcp.json", "gitignored") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected gitignore warning for .mcp.json, got: %v", r.Warnings)
	}
}
