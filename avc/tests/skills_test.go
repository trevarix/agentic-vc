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

	r, err := skills.Write(project, skills.FrameworkClaudeCode)
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

	// Global config must never be created.
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Error("Write created ~/.claude.json")
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

func TestSkillsCursorProjectLevel(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()

	if _, err := skills.Write(project, skills.FrameworkCursor); err != nil {
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

	if _, err := skills.Write(project, skills.FrameworkClaudeCode); err != nil {
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
	r, err := skills.Write(project, skills.FrameworkClaudeCode)
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

	r, err := skills.Write(project, skills.FrameworkClaudeCode)
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

// gitProject returns a temp project containing a .git directory, so
// AVC's gitignore logic treats it as a git repository.
func gitProject(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	return project
}

func readGitignore(t *testing.T, project string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(project, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	return string(data)
}

// TestSkills_CreatedFilesAreGitignored verifies that every project file AVC
// creates is added to .gitignore — generated agent files are local tooling.
func TestSkills_CreatedFilesAreGitignored(t *testing.T) {
	fakeHome(t)
	project := gitProject(t)

	if _, err := skills.Write(project, skills.FrameworkClaudeCode); err != nil {
		t.Fatalf("Write: %v", err)
	}

	content := readGitignore(t, project)
	for _, entry := range []string{".mcp.json", "CLAUDE.md", ".claude/skills/avc-*/"} {
		if !strings.Contains(content, entry+"\n") {
			t.Errorf(".gitignore missing %q:\n%s", entry, content)
		}
	}
	if strings.Count(content, "# Agentic Version Control") != 1 {
		t.Errorf("expected exactly one AVC header:\n%s", content)
	}
}

// TestSkills_PreexistingFilesAreNotGitignored verifies the ownership rule:
// when the user authored the project's .gitignore, files that existed before
// AVC ran (user-owned, merely appended to or merged into) and were not
// gitignored must not become gitignored.
func TestSkills_PreexistingFilesAreNotGitignored(t *testing.T) {
	fakeHome(t)
	project := gitProject(t)

	// The user authored the .gitignore — their tracking policy stands.
	if err := os.WriteFile(filepath.Join(project, ".gitignore"), []byte("node_modules/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// User-owned CLAUDE.md and .mcp.json exist before AVC runs.
	if err := os.WriteFile(filepath.Join(project, "CLAUDE.md"), []byte("# My project\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pre := `{"mcpServers":{"other":{"command":"other-tool"}}}`
	if err := os.WriteFile(filepath.Join(project, ".mcp.json"), []byte(pre), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := skills.Write(project, skills.FrameworkClaudeCode); err != nil {
		t.Fatalf("Write: %v", err)
	}

	content := readGitignore(t, project)
	for _, entry := range []string{"CLAUDE.md", ".mcp.json"} {
		if strings.Contains(content, entry+"\n") {
			t.Errorf("pre-existing %q must not be gitignored:\n%s", entry, content)
		}
	}
	// Skill files were still created by AVC — they are gitignored.
	if !strings.Contains(content, ".claude/skills/avc-*/\n") {
		t.Errorf(".gitignore missing skill-dir pattern:\n%s", content)
	}

	// The merge preserved the user's server, and the append kept their content.
	servers := readMCPServers(t, filepath.Join(project, ".mcp.json"))
	if _, ok := servers["other"]; !ok {
		t.Error("merge dropped the user's pre-existing server entry")
	}
	claudeMD, err := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if err != nil || !strings.Contains(string(claudeMD), "# My project") {
		t.Error("append lost the user's CLAUDE.md content")
	}
}

// TestSkills_NoUserGitignore_PreexistingFilesAreGitignored verifies that when
// the user has no .gitignore of their own (none, or only the AVC-created one),
// there is no expressed tracking policy — so ALL AVC-touched project files are
// gitignored, including pre-existing ones AVC appended to.
func TestSkills_NoUserGitignore_PreexistingFilesAreGitignored(t *testing.T) {
	fakeHome(t)
	project := gitProject(t)

	if err := os.WriteFile(filepath.Join(project, "CLAUDE.md"), []byte("# My project\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := skills.Write(project, skills.FrameworkClaudeCode); err != nil {
		t.Fatalf("Write: %v", err)
	}

	content := readGitignore(t, project)
	for _, entry := range []string{"CLAUDE.md", ".mcp.json", ".claude/skills/avc-*/"} {
		if !strings.Contains(content, entry+"\n") {
			t.Errorf(".gitignore missing %q with no user gitignore:\n%s", entry, content)
		}
	}
	// The user's CLAUDE.md content is still preserved (append, not overwrite).
	claudeMD, err := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if err != nil || !strings.Contains(string(claudeMD), "# My project") {
		t.Error("append lost the user's CLAUDE.md content")
	}
}

// TestSkills_CursorCreatedFilesAreGitignored covers the cursor framework's
// created files.
func TestSkills_CursorCreatedFilesAreGitignored(t *testing.T) {
	fakeHome(t)
	project := gitProject(t)

	if _, err := skills.Write(project, skills.FrameworkCursor); err != nil {
		t.Fatalf("Write: %v", err)
	}

	content := readGitignore(t, project)
	for _, entry := range []string{".cursor/mcp.json", ".cursor/rules/avc.mdc"} {
		if !strings.Contains(content, entry+"\n") {
			t.Errorf(".gitignore missing %q:\n%s", entry, content)
		}
	}
}

// TestSkills_NoGit_NoGitignoreCreated verifies that outside a git repository
// no .gitignore is created.
func TestSkills_NoGit_NoGitignoreCreated(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()

	if _, err := skills.Write(project, skills.FrameworkClaudeCode); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".gitignore")); !os.IsNotExist(err) {
		t.Error("Write created .gitignore outside a git repository")
	}
}
