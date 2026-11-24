// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Tests for the agent-tool integrations written by avc init --skills:
// Windsurf, Codex, Copilot and generic setups, rule refresh, sharing,
// hooks, doctor, and multi-harness hook payloads.
package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/trevarix/agentic-vc/avc/internal/agenthook"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/skills"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func writeFW(t *testing.T, project, fw string, opts skills.Options) *skills.WriteResult {
	t.Helper()
	r, err := skills.WriteWithOptions(project, fw, opts)
	if err != nil {
		t.Fatalf("Write %s: %v", fw, err)
	}
	return r
}

func statusOf(r *skills.WriteResult, path string) string {
	for _, a := range r.Actions {
		if a.Path == path {
			return a.Status
		}
	}
	return ""
}

func TestValidateRejectsUnknownFramework(t *testing.T) {
	if err := skills.Validate([]string{"codex", "copilot", "windsurf", "generic"}); err != nil {
		t.Errorf("supported frameworks rejected: %v", err)
	}
	if err := skills.Validate([]string{"codex", "codx"}); err == nil || !strings.Contains(err.Error(), "codx") {
		t.Errorf("expected an error naming 'codx', got %v", err)
	}
}

// TestDefaultLeavesAgentFilesTrackable verifies that, without --local, agent
// files stay out of .gitignore so teammates who clone get the setup.
func TestDefaultLeavesAgentFilesTrackable(t *testing.T) {
	fakeHome(t)
	project := gitProject(t)
	for _, fw := range []string{skills.FrameworkCodex, skills.FrameworkCopilot, skills.FrameworkGeneric} {
		writeFW(t, project, fw, skills.Options{})
	}
	if data, err := os.ReadFile(filepath.Join(project, ".gitignore")); err == nil {
		for _, f := range []string{"AGENTS.md", ".codex/config.toml", ".vscode/mcp.json"} {
			if strings.Contains(string(data), f) {
				t.Errorf("%s gitignored by default:\n%s", f, data)
			}
		}
	}
}

func TestCursorRulesAlwaysApplyAndLegacyRefresh(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()
	rules := filepath.Join(project, ".cursor", "rules", "avc.mdc")

	// A rules file from an older AVC version: no frontmatter, no marker.
	mustWrite(t, rules, "# AVC — Agentic Version Control\n\nAVC is active on this project. Old rules.\n")
	r := writeFW(t, project, skills.FrameworkCursor, skills.Options{})
	if got := statusOf(r, ".cursor/rules/avc.mdc"); got != "updated" {
		t.Errorf("legacy avc.mdc status = %q, want updated", got)
	}
	content := mustRead(t, rules)
	if !strings.HasPrefix(content, "---\n") || !strings.Contains(content, "alwaysApply: true") {
		t.Errorf("avc.mdc lacks alwaysApply frontmatter:\n%s", content)
	}

	// A file the user wrote is never overwritten.
	mustWrite(t, rules, "my own cursor rules\n")
	r = writeFW(t, project, skills.FrameworkCursor, skills.Options{})
	if got := statusOf(r, ".cursor/rules/avc.mdc"); got != "skipped" {
		t.Errorf("user avc.mdc status = %q, want skipped", got)
	}
	if mustRead(t, rules) != "my own cursor rules\n" {
		t.Error("user-owned avc.mdc was overwritten")
	}
}

// TestWindsurfProjectConfigForDevinDesktop covers Windsurf after its rename to
// Devin Desktop: the server goes in the project-level .devin/mcp_config.json,
// and no global config is created when no pre-rename Windsurf is installed.
func TestWindsurfProjectConfigForDevinDesktop(t *testing.T) {
	home := fakeHome(t)
	project := t.TempDir()
	writeFW(t, project, skills.FrameworkWindsurf, skills.Options{})

	servers := readMCPServers(t, filepath.Join(project, ".devin", "mcp_config.json"))
	entry, ok := servers["avc"].(map[string]any)
	if !ok || entry["command"] == "" {
		t.Fatalf(".devin/mcp_config.json has no avc entry: %v", servers)
	}
	if _, err := os.Stat(filepath.Join(home, ".codeium")); !os.IsNotExist(err) {
		t.Error("created a pre-rename Windsurf global config on a machine without Windsurf")
	}
}

func TestWindsurfGlobalRootsAndRulesMigration(t *testing.T) {
	home := fakeHome(t)
	projectA, projectB := t.TempDir(), t.TempDir()
	// A pre-rename Windsurf install keeps its global config here.
	if err := os.MkdirAll(filepath.Join(home, ".codeium", "windsurf"), 0755); err != nil {
		t.Fatal(err)
	}

	// Legacy rules block alongside the user's own Windsurf rules.
	legacy := filepath.Join(projectA, ".windsurfrules")
	mustWrite(t, legacy, "Use tabs.\n\n# AVC — Agentic Version Control\n\nold rules\n<!-- /AVC — Agentic Version Control -->\n")

	writeFW(t, projectA, skills.FrameworkWindsurf, skills.Options{})
	writeFW(t, projectB, skills.FrameworkWindsurf, skills.Options{})

	servers := readMCPServers(t, filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"))
	args, _ := servers["avc"].(map[string]any)["args"].([]any)
	joined := ""
	for _, a := range args {
		joined += a.(string) + "\n"
	}
	if !strings.Contains(joined, projectA) || !strings.Contains(joined, projectB) {
		t.Errorf("global entry should search both projects, args = %v", args)
	}

	rules := mustRead(t, filepath.Join(projectA, ".windsurf", "rules", "avc.md"))
	if !strings.HasPrefix(rules, "---\ntrigger: always_on\n---") {
		t.Errorf("windsurf rules lack always_on trigger:\n%s", rules)
	}
	left := mustRead(t, legacy)
	if strings.Contains(left, "AVC") || !strings.Contains(left, "Use tabs.") {
		t.Errorf("legacy .windsurfrules should keep only the user's rules:\n%s", left)
	}
}

func TestCodexConfigMergePreservesUserConfig(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()
	cfgPath := filepath.Join(project, ".codex", "config.toml")
	mustWrite(t, cfgPath, "# my settings\nmodel = \"o4\"\n\n[mcp_servers.other]\ncommand = \"other\"\n")

	r := writeFW(t, project, skills.FrameworkCodex, skills.Options{})
	if got := statusOf(r, ".codex/config.toml"); got != "updated" {
		t.Errorf("config.toml status = %q, want updated", got)
	}
	content := mustRead(t, cfgPath)
	if !strings.Contains(content, "# my settings") {
		t.Error("user comment was dropped")
	}
	var cfg struct {
		Model      string `toml:"model"`
		MCPServers map[string]struct {
			Command string   `toml:"command"`
			Args    []string `toml:"args"`
		} `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(content, &cfg); err != nil {
		t.Fatalf("result does not parse: %v\n%s", err, content)
	}
	if cfg.Model != "o4" || cfg.MCPServers["other"].Command != "other" {
		t.Errorf("user settings lost: %+v", cfg)
	}
	if avc := cfg.MCPServers["avc"]; avc.Command == "" || len(avc.Args) < 2 || avc.Args[1] != "serve" {
		t.Errorf("avc server entry wrong: %+v", avc)
	}
	if !strings.Contains(mustRead(t, filepath.Join(project, "AGENTS.md")), "avc_snapshot") {
		t.Error("AGENTS.md has no AVC rules")
	}

	r = writeFW(t, project, skills.FrameworkCodex, skills.Options{})
	if got := statusOf(r, ".codex/config.toml"); got != "skipped" {
		t.Errorf("second run status = %q, want skipped", got)
	}
}

func TestCopilotVSCodeConfigAndJSONCSafety(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()
	r0 := writeFW(t, project, skills.FrameworkCopilot, skills.Options{})

	// The Copilot agent reads the portable .mcp.json (mcpServers).
	portable := readMCPServers(t, filepath.Join(project, ".mcp.json"))
	if e, ok := portable["avc"].(map[string]any); !ok || e["type"] != "stdio" {
		t.Fatalf(".mcp.json needs mcpServers.avc with type stdio, got %v", portable)
	}

	// VS Code's older Local mode reads .vscode/mcp.json (servers).
	var cfg map[string]any
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(project, ".vscode", "mcp.json"))), &cfg); err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg["servers"].(map[string]any)["avc"].(map[string]any)
	if !ok || entry["type"] != "stdio" {
		t.Fatalf(`.vscode/mcp.json needs servers.avc with type stdio, got %v`, cfg)
	}
	if _, set := entry["cwd"]; set {
		t.Error(".vscode/mcp.json should not set cwd: VS Code defaults it to the workspace folder")
	}

	// The coding agent reads no file; the next steps give its settings JSON.
	if !strings.Contains(strings.Join(r0.NextSteps, "\n"), `"type": "local"`) {
		t.Errorf("next steps lack the coding agent MCP JSON: %v", r0.NextSteps)
	}
	instr := mustRead(t, filepath.Join(project, ".github", "instructions", "avc.instructions.md"))
	if !strings.Contains(instr, `applyTo: "**"`) {
		t.Errorf("copilot instructions lack applyTo:\n%s", instr)
	}

	// A config with comments is left alone, with a warning.
	other := t.TempDir()
	jsonc := "{\n  // my servers\n  \"servers\": {}\n}\n"
	mustWrite(t, filepath.Join(other, ".vscode", "mcp.json"), jsonc)
	r := writeFW(t, other, skills.FrameworkCopilot, skills.Options{})
	if mustRead(t, filepath.Join(other, ".vscode", "mcp.json")) != jsonc {
		t.Error("commented mcp.json was rewritten")
	}
	if len(r.Warnings) == 0 {
		t.Error("expected a warning telling the user to add the entry by hand")
	}
}

func TestGenericAgentsMDBlockUpsert(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()
	agents := filepath.Join(project, "AGENTS.md")
	mustWrite(t, agents, "# Project\n\nBuild with make.\n")

	writeFW(t, project, skills.FrameworkGeneric, skills.Options{})
	first := mustRead(t, agents)
	if !strings.HasPrefix(first, "# Project\n\nBuild with make.\n") || strings.Count(first, "<!-- AVC") != 1 {
		t.Errorf("AVC block not appended once:\n%s", first)
	}
	if _, err := os.Stat(filepath.Join(project, "AGENT_INSTRUCTIONS.md")); !os.IsNotExist(err) {
		t.Error("generic should no longer write AGENT_INSTRUCTIONS.md")
	}

	// A stale block is replaced in place; the user's text stays.
	stale := strings.Replace(first, "avc_snapshot", "avc_old_tool", 1)
	mustWrite(t, agents, stale)
	r := writeFW(t, project, skills.FrameworkGeneric, skills.Options{})
	if got := statusOf(r, "AGENTS.md"); got != "updated" {
		t.Errorf("stale block status = %q, want updated", got)
	}
	if mustRead(t, agents) != first {
		t.Error("refreshed AGENTS.md differs from a fresh write")
	}
}

// TestRulesFollowConfiguredTier keeps rules and tools in agreement: the run
// tool is described only when the configured tier exposes it.
func TestRulesFollowConfiguredTier(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()
	writeFW(t, project, skills.FrameworkCopilot, skills.Options{})
	rulesPath := filepath.Join(project, ".github", "instructions", "avc.instructions.md")
	if strings.Contains(mustRead(t, rulesPath), "avc_run_in_workspace") {
		t.Error("standard-tier rules mention avc_run_in_workspace, which standard does not expose")
	}

	cfgPath := filepath.Join(project, ".mcp.json")
	mustWrite(t, cfgPath, strings.Replace(mustRead(t, cfgPath), `"standard"`, `"full"`, 1))
	writeFW(t, project, skills.FrameworkCopilot, skills.Options{})
	if !strings.Contains(mustRead(t, rulesPath), "avc_run_in_workspace") {
		t.Error("full-tier rules omit avc_run_in_workspace")
	}
}

func TestHooksInstalledPerHarnessAndIdempotent(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()
	// An existing Windsurf hook must survive.
	mustWrite(t, filepath.Join(project, ".windsurf", "hooks.json"),
		`{"hooks":{"pre_write_code":[{"command":"lint"}]}}`)

	// Each harness's hooks files, in the format its documentation gives.
	cases := []struct {
		fw, rel, want string
	}{
		{skills.FrameworkWindsurf, ".devin/hooks.v1.json", `"PreToolUse"`},
		{skills.FrameworkWindsurf, ".windsurf/hooks.json", `"pre_write_code"`},
		{skills.FrameworkCodex, ".codex/hooks.json", `"apply_patch|Edit|Write"`},
		{skills.FrameworkCopilot, ".github/hooks/avc.json", `"version": 1`},
		{skills.FrameworkCursor, ".cursor/hooks.json", `"preToolUse"`},
	}
	for _, c := range cases {
		writeFW(t, project, c.fw, skills.Options{Hooks: true})
		content := mustRead(t, filepath.Join(project, filepath.FromSlash(c.rel)))
		if !strings.Contains(content, "avc hook pre-edit --agent "+c.fw) || !strings.Contains(content, c.want) {
			t.Errorf("%s: hook missing --agent or %s:\n%s", c.rel, c.want, content)
		}
		r := writeFW(t, project, c.fw, skills.Options{Hooks: true})
		if got := statusOf(r, c.rel); got != "skipped" {
			t.Errorf("%s second run status = %q, want skipped", c.rel, got)
		}
	}
	if !strings.Contains(mustRead(t, filepath.Join(project, ".windsurf", "hooks.json")), `"lint"`) {
		t.Error("existing Windsurf hook was dropped")
	}
	// Devin's hooks.v1.json holds the event map directly, with no wrapper.
	if strings.Contains(mustRead(t, filepath.Join(project, ".devin", "hooks.v1.json")), `"hooks": {`) {
		t.Error(".devin/hooks.v1.json must not wrap events in a hooks object")
	}
}

// TestHarnessReplyKeepsHooksFromBlockingEdits checks what the pre-edit hook
// prints for each harness: Cursor's preToolUse treats non-JSON as a denial,
// so it gets an explicit allow; the others read stdout as a decision too, so
// they get nothing.
func TestHarnessReplyKeepsHooksFromBlockingEdits(t *testing.T) {
	if out, quiet := agenthook.HarnessReply("cursor"); !quiet || out != `{"permission":"allow"}` {
		t.Errorf("cursor reply = %q quiet=%v, want an allow JSON", out, quiet)
	}
	for _, h := range []string{"codex", "copilot", "windsurf"} {
		if out, quiet := agenthook.HarnessReply(h); !quiet || out != "" {
			t.Errorf("%s reply = %q quiet=%v, want silence on stdout", h, out, quiet)
		}
	}
	if _, quiet := agenthook.HarnessReply(""); quiet {
		t.Error("Claude Code's hook (no --agent) should keep its readable report")
	}
}

func TestDoctorFlagsStaleRulesAndMissingCommand(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()
	writeFW(t, project, skills.FrameworkCopilot, skills.Options{})

	byName := func() map[string]skills.Check {
		m := map[string]skills.Check{}
		for _, rep := range skills.Diagnose(project) {
			if rep.Framework == skills.FrameworkCopilot {
				for _, c := range rep.Checks {
					m[c.Name] = c
				}
			}
		}
		return m
	}
	if c := byName()["rules"]; c.Status != skills.StatusOK {
		t.Errorf("fresh rules: %+v", c)
	}

	rulesPath := filepath.Join(project, ".github", "instructions", "avc.instructions.md")
	mustWrite(t, rulesPath, mustRead(t, rulesPath)+"\nedited\n")
	if c := byName()["rules"]; c.Status != skills.StatusWarn {
		t.Errorf("edited rules should warn: %+v", c)
	}

	cfgPath := filepath.Join(project, ".mcp.json")
	mustWrite(t, cfgPath, `{"mcpServers":{"avc":{"type":"stdio","command":"C:/no/such/avc.exe","args":["mcp","serve"]}}}`)
	if c := byName()["mcp server"]; c.Status != skills.StatusFail {
		t.Errorf("missing command should fail: %+v", c)
	}
}

func TestParseInputAcrossHarnesses(t *testing.T) {
	project := t.TempDir()
	file := filepath.Join(project, "src", "app.py")
	fileJSON, _ := json.Marshal(file)

	cases := []struct {
		name, payload, session, cwd string
	}{
		{"claude/codex", `{"session_id":"s1","cwd":"` + filepath.ToSlash(project) + `","tool_name":"Edit"}`, "s1", filepath.ToSlash(project)},
		{"copilot camelCase", `{"sessionId":"s2","cwd":"/w"}`, "s2", "/w"},
		{"windsurf", `{"agent_action_name":"pre_write_code","trajectory_id":"t3","tool_info":{"file_path":` + string(fileJSON) + `}}`, "t3", filepath.Dir(file)},
		{"cursor", `{"conversation_id":"c4","workspace_roots":["/r"]}`, "c4", "/r"},
		{"empty", "  \n", "", ""},
	}
	for _, c := range cases {
		in, err := agenthook.ParseInput([]byte(c.payload))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if in.SessionID != c.session || in.CWD != c.cwd {
			t.Errorf("%s: got session %q cwd %q, want %q %q", c.name, in.SessionID, in.CWD, c.session, c.cwd)
		}
	}
}

// TestPreEditAttributesHarness checks the checkpoint names the tool that ran it.
func TestPreEditAttributesHarness(t *testing.T) {
	project, _ := setupProjectWithMain(t)
	got, err := agenthook.PreEdit(agenthook.Input{SessionID: "ws-1", CWD: project, Harness: "windsurf"})
	if err != nil || got.Action != agenthook.ActionSnapshotted {
		t.Fatalf("PreEdit: %+v %v", got, err)
	}
	store, err := db.Open(project)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snaps, err := store.ListSnapshotsFiltered(db.SnapshotFilter{SessionID: "ws-1", Limit: 1})
	if err != nil || len(snaps) != 1 {
		t.Fatalf("list: %v (%d)", err, len(snaps))
	}
	if snaps[0].AgentName != "windsurf-hook" {
		t.Errorf("checkpoint agent = %q, want windsurf-hook", snaps[0].AgentName)
	}
}
