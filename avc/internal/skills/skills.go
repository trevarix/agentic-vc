// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package skills writes MCP server configs and agent instruction files
// for the frameworks supported by `avc init --skills <framework>`.
package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Supported framework identifiers.
const (
	FrameworkClaudeCode    = "claude-code"
	FrameworkClaudeDesktop = "claude-desktop"
	FrameworkCursor        = "cursor"
	FrameworkWindsurf      = "windsurf"
	FrameworkGeneric       = "generic"
)

// SupportedFrameworks lists all valid --skills values.
var SupportedFrameworks = []string{
	FrameworkClaudeCode,
	FrameworkClaudeDesktop,
	FrameworkCursor,
	FrameworkWindsurf,
	FrameworkGeneric,
}

// FileAction records what happened to a single file.
type FileAction struct {
	Path   string // relative to projectRoot
	Status string // "created" | "updated" | "skipped"
	Reason string // non-empty when Status == "skipped"
}

// WriteResult records actions and warnings for one framework.
type WriteResult struct {
	Framework string
	Actions   []FileAction
	Warnings  []string
}

// Write installs MCP config and agent instructions for the given framework.
func Write(projectRoot, framework string) (*WriteResult, error) {
	r := &WriteResult{Framework: framework}

	// Warn about any top-level directories we will write into that are gitignored.
	checkGitignoreWarnings(projectRoot, framework, r)

	switch framework {
	case FrameworkClaudeCode:
		if err := writeClaudeCode(projectRoot, r); err != nil {
			return nil, err
		}
	case FrameworkClaudeDesktop:
		if err := writeClaudeDesktop(projectRoot, r); err != nil {
			return nil, err
		}
	case FrameworkCursor:
		if err := writeCursor(projectRoot, r); err != nil {
			return nil, err
		}
	case FrameworkWindsurf:
		if err := writeWindsurf(projectRoot, r); err != nil {
			return nil, err
		}
	case FrameworkGeneric:
		if err := writeGeneric(projectRoot, r); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown framework '%s'; supported: %s",
			framework, strings.Join(SupportedFrameworks, ", "))
	}
	return r, nil
}

// ─── Framework writers ────────────────────────────────────────────────────────

func writeClaudeCode(projectRoot string, r *WriteResult) error {
	// MCP config — ~/.claude/settings.json (global, loaded by all Claude Code interfaces)
	globalSettings, err := claudeGlobalSettingsPath()
	if err != nil {
		return err
	}
	action, err := mergeMCPConfig(globalSettings, "avc", r)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(globalSettings, action))

	// CLAUDE.md — always-loaded context (append, idempotent)
	claudeMDPath := filepath.Join(projectRoot, "CLAUDE.md")
	action, err = appendRulesBlock(claudeMDPath, claudeMDBlock)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction("CLAUDE.md", action))

	// Skill files — .claude/skills/avc-<name>/SKILL.md
	for name, content := range claudeSkillFiles {
		rel := ".claude/skills/" + name + "/SKILL.md"
		path := filepath.Join(projectRoot, filepath.FromSlash(rel))
		action, err := writeFileIfAbsent(path, content)
		if err != nil {
			return err
		}
		r.Actions = append(r.Actions, fileAction(rel, action))
	}
	return nil
}

func writeClaudeDesktop(projectRoot string, r *WriteResult) error {
	// MCP config — OS-specific Claude Desktop config file.
	// Use "avc:<project-name>" as the server key so multiple projects can
	// coexist in the config without overwriting each other.
	desktopConfig, err := claudeDesktopConfigPath()
	if err != nil {
		return err
	}
	serverKey := "avc:" + filepath.Base(projectRoot)
	action, err := mergeMCPConfigWithEnv(desktopConfig, serverKey, projectRoot, r)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(desktopConfig, action))

	// CLAUDE.md — always-loaded context (append, idempotent)
	claudeMDPath := filepath.Join(projectRoot, "CLAUDE.md")
	action, err = appendRulesBlock(claudeMDPath, claudeMDBlock)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction("CLAUDE.md", action))

	// Skill files — .claude/skills/avc-<name>/SKILL.md
	for name, content := range claudeSkillFiles {
		rel := ".claude/skills/" + name + "/SKILL.md"
		path := filepath.Join(projectRoot, filepath.FromSlash(rel))
		action, err := writeFileIfAbsent(path, content)
		if err != nil {
			return err
		}
		r.Actions = append(r.Actions, fileAction(rel, action))
	}
	return nil
}

func writeCursor(projectRoot string, r *WriteResult) error {
	// MCP config — ~/.cursor/mcp.json (global, loaded by all Cursor interfaces)
	globalMCP, err := cursorGlobalMCPPath()
	if err != nil {
		return err
	}
	action, err := mergeMCPConfig(globalMCP, "avc", r)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(globalMCP, action))

	// Rules file — .cursor/rules/avc.mdc (project-level, checked into repo)
	rulesPath := filepath.Join(projectRoot, ".cursor", "rules", "avc.mdc")
	action, err = writeFileIfAbsent(rulesPath, cursorRulesContent)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(".cursor/rules/avc.mdc", action))
	return nil
}

func writeWindsurf(projectRoot string, r *WriteResult) error {
	// MCP config — ~/.codeium/windsurf/mcp_config.json (global, loaded by all Windsurf interfaces)
	globalMCP, err := windsurfGlobalMCPPath()
	if err != nil {
		return err
	}
	action, err := mergeMCPConfig(globalMCP, "avc", r)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(globalMCP, action))

	// Rules file — .windsurfrules (append, idempotent, project-level)
	rulesPath := filepath.Join(projectRoot, ".windsurfrules")
	action, err = appendRulesBlock(rulesPath, windsurfRulesBlock)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(".windsurfrules", action))
	return nil
}

func writeGeneric(projectRoot string, r *WriteResult) error {
	path := filepath.Join(projectRoot, "AGENT_INSTRUCTIONS.md")
	action, err := writeFileIfAbsent(path, genericInstructions)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction("AGENT_INSTRUCTIONS.md", action))
	return nil
}

// fileAction constructs a FileAction from a relative path and raw action string.
// action is one of "created", "updated", "skipped:already exists",
// "skipped:already configured", "skipped:marker present".
func fileAction(rel, action string) FileAction {
	parts := strings.SplitN(action, ":", 2)
	fa := FileAction{Path: rel, Status: parts[0]}
	if len(parts) == 2 {
		fa.Reason = parts[1]
	}
	return fa
}

// ─── Content: CLAUDE.md block ────────────────────────────────────────────────

const claudeMDMarker = "<!-- AVC — Agentic Version Control -->"

const claudeMDBlock = `
<!-- AVC — Agentic Version Control -->
## AVC — Agentic Version Control

AVC is active on this project. You MUST use it. The MCP server starts automatically and exposes ` + "`avc_*`" + ` tools.

### Mandatory actions

**ALWAYS call ` + "`avc_snapshot`" + ` before making any code change. No exceptions.**
Do not assess whether the change is "small enough" or "easily reversible" — that judgement is not yours to make. The cost of an unnecessary snapshot is negligible. The cost of skipping one when it was needed is not.

Label format — always use the ` + "`auto:`" + ` prefix so agent snapshots are distinguishable in ` + "`avc list`" + `:
- ` + "`auto: before <2–5 word description>`" + `

**ALWAYS call ` + "`avc_branch_create`" + ` before starting any task. No exceptions.**
Do not assess whether the task is "simple enough" to skip a branch — that judgement is not yours to make. After creating a branch, set your working directory to the ` + "`workspace`" + ` path in the response. NEVER edit files in the real project root while on a branch.

**ALWAYS call ` + "`avc_restore`" + ` when:**
- Tests fail after your changes
- The build breaks
- The user says "undo", "revert", "roll back", or "start over"
- Do NOT attempt repeated fixes on broken state — restore first, then retry.

**NEVER call ` + "`avc_merge`" + `** without the user saying yes explicitly.
` + "`avc_merge`" + ` checks for conflicts automatically before writing anything — no separate preview step needed. If conflicts are found, it returns them without modifying main.

**NEVER call ` + "`avc_run_in_workspace`" + `** without first:
1. Showing the user the exact command you intend to run
2. Explaining what it does
3. Receiving explicit approval

### Quick reference

| Trigger | Tool |
|---------|------|
| About to make changes | ` + "`avc_snapshot`" + ` |
| Multi-step or multi-file task | ` + "`avc_branch_create`" + ` |
| Something broke | ` + "`avc_restore`" + ` |
| Ready to review branch work | ` + "`avc_branch_diff`" + ` |
| Ready to merge (with approval) | ` + "`avc_merge`" + ` |
| Need to run tests or build | ` + "`avc_run_in_workspace`" + ` (approval required) |
`

// ─── Claude Code global settings path ────────────────────────────────────────

// claudeGlobalSettingsPath returns the path to ~/.claude.json.
// This is the global Claude Code config file read by all interfaces (VSCode
// extension, desktop app, CLI) regardless of the current project or CWD.
func claudeGlobalSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".claude.json"), nil
}

// claudeDesktopConfigPath returns the OS-specific path to the Claude Desktop
// MCP config file.
//
//	macOS:   ~/Library/Application Support/Claude/claude_desktop_config.json
//	Windows: %APPDATA%\Claude\claude_desktop_config.json
//	Linux:   ~/.config/Claude/claude_desktop_config.json
func claudeDesktopConfigPath() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", fmt.Errorf("APPDATA environment variable not set")
		}
		return filepath.Join(appdata, "Claude", "claude_desktop_config.json"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"), nil
	}
}

// cursorGlobalMCPPath returns the path to ~/.cursor/mcp.json.
// This file is loaded by all Cursor interfaces regardless of project CWD.
func cursorGlobalMCPPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".cursor", "mcp.json"), nil
}

// windsurfGlobalMCPPath returns the path to ~/.codeium/windsurf/mcp_config.json.
// This file is loaded by all Windsurf interfaces regardless of project CWD.
func windsurfGlobalMCPPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"), nil
}

// ─── MCP config helpers ───────────────────────────────────────────────────────

// resolveBinaryPath returns the absolute path to the running avc binary.
// EvalSymlinks is applied so Homebrew and similar symlink installations resolve
// to the real binary path that the OS can execute without a shell PATH lookup.
// Returns ("avc", true) if the path cannot be resolved or looks like a temp
// directory (i.e. the binary was run via `go run` and won't be stable).
func resolveBinaryPath() (path string, isTmp bool) {
	exe, err := os.Executable()
	if err != nil {
		return "avc", false
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	// Warn callers when the binary lives in a temp directory — this happens
	// when the user runs `go run .` instead of a real installed binary.
	tmp := os.TempDir()
	if strings.HasPrefix(resolved, tmp) {
		return resolved, true
	}
	return resolved, false
}

// mergeMCPConfig merges the AVC server entry into the JSON file at path.
// Uses the absolute path of the running binary so the agent framework can
// start the server without relying on the user's shell PATH.
// Returns "created", "updated", or "skipped:already configured".
func mergeMCPConfig(path, serverKey string, r *WriteResult) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	binaryPath, isTmp := resolveBinaryPath()
	if isTmp {
		r.Warnings = append(r.Warnings,
			"avc binary appears to be a temporary build (go run) — the MCP server path in the config may not be stable. Install avc to a permanent location and re-run `avc init --skills`.")
	}

	entry := map[string]any{
		"command": binaryPath,
		"args":    []string{"mcp", "serve"},
	}

	var root map[string]any
	existed := false
	data, err := os.ReadFile(path)
	if err == nil {
		existed = true
		if jsonErr := json.Unmarshal(data, &root); jsonErr != nil {
			return "", fmt.Errorf("parse %s: %w", path, jsonErr)
		}
	} else if os.IsNotExist(err) {
		root = map[string]any{}
	} else {
		return "", err
	}

	servers, ok := root["mcpServers"].(map[string]any)
	if !ok {
		servers = map[string]any{}
		root["mcpServers"] = servers
	}

	if existing, exists := servers[serverKey]; exists {
		// If the command path already matches the current binary, nothing to do.
		if existingMap, ok := existing.(map[string]any); ok {
			if existingMap["command"] == binaryPath {
				return "skipped:already configured", nil
			}
		}
		// Path differs (bare "avc", old install location, etc.) — update it.
		servers[serverKey] = entry
		out, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(path, append(out, '\n'), 0644); err != nil {
			return "", err
		}
		return "updated", nil
	}

	servers[serverKey] = entry
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0644); err != nil {
		return "", err
	}

	if existed {
		return "updated", nil
	}
	return "created", nil
}

// mergeMCPConfigWithEnv is like mergeMCPConfig but also writes an env block
// with AVC_PROJECT set to projectRoot. Used for Claude Desktop, which spawns
// the MCP server without a meaningful CWD, so the project root must be passed
// explicitly via environment variable.
func mergeMCPConfigWithEnv(path, serverKey, projectRoot string, r *WriteResult) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	binaryPath, isTmp := resolveBinaryPath()
	if isTmp {
		r.Warnings = append(r.Warnings,
			"avc binary appears to be a temporary build (go run) — the MCP server path in the config may not be stable. Install avc to a permanent location and re-run `avc init --skills`.")
	}

	entry := map[string]any{
		"command": binaryPath,
		"args":    []string{"mcp", "serve"},
		"env":     map[string]any{"AVC_PROJECT": projectRoot},
	}

	var root map[string]any
	existed := false
	data, err := os.ReadFile(path)
	if err == nil {
		existed = true
		if jsonErr := json.Unmarshal(data, &root); jsonErr != nil {
			return "", fmt.Errorf("parse %s: %w", path, jsonErr)
		}
	} else if os.IsNotExist(err) {
		root = map[string]any{}
	} else {
		return "", err
	}

	servers, ok := root["mcpServers"].(map[string]any)
	if !ok {
		servers = map[string]any{}
		root["mcpServers"] = servers
	}

	if existing, exists := servers[serverKey]; exists {
		if existingMap, ok := existing.(map[string]any); ok {
			if existingMap["command"] == binaryPath {
				// Check if the project root also matches before skipping.
				if envMap, ok := existingMap["env"].(map[string]any); ok {
					if envMap["AVC_PROJECT"] == projectRoot {
						return "skipped:already configured", nil
					}
				}
			}
		}
		servers[serverKey] = entry
		out, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(path, append(out, '\n'), 0644); err != nil {
			return "", err
		}
		return "updated", nil
	}

	servers[serverKey] = entry
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0644); err != nil {
		return "", err
	}

	if existed {
		return "updated", nil
	}
	return "created", nil
}

// ─── Rules-file helpers ───────────────────────────────────────────────────────

const avcRulesMarker = "# AVC — Agentic Version Control"

// appendRulesBlock appends the AVC block to a rules file.
// Returns "created", "updated", or "skipped:marker present".
// Checks for both the markdown marker and the HTML comment marker so it works
// correctly for both .windsurfrules and CLAUDE.md.
func appendRulesBlock(path, block string) (string, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	content := string(existing)
	if strings.Contains(content, avcRulesMarker) || strings.Contains(content, claudeMDMarker) {
		return "skipped:marker present", nil
	}

	existed := len(existing) > 0
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	prefix := ""
	if existed && existing[len(existing)-1] != '\n' {
		prefix = "\n"
	}
	if _, err := f.WriteString(prefix + block); err != nil {
		return "", err
	}

	if existed {
		return "updated", nil
	}
	return "created", nil
}

// ─── File helper ─────────────────────────────────────────────────────────────

// writeFileIfAbsent writes content only if path does not exist.
// Returns "created" or "skipped:already exists".
func writeFileIfAbsent(path, content string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return "skipped:already exists", nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return "created", nil
}

// ─── Gitignore detection ──────────────────────────────────────────────────────

// frameworkDirs maps each framework to project-level directories it writes into.
// Global config paths (home directory) are excluded — they exist outside the project.
var frameworkDirs = map[string][]string{
	FrameworkClaudeCode:    {".claude"},
	FrameworkClaudeDesktop: {".claude"},
	FrameworkCursor:        {".cursor"}, // rules file goes here; MCP config is global
	FrameworkWindsurf:      {},          // .windsurfrules is at project root; MCP config is global
	FrameworkGeneric:       {},          // writes to project root — no subdirectory to check
}

// checkGitignoreWarnings reads .gitignore and adds a warning to r for any
// directory we are about to write into that is covered by a gitignore pattern.
func checkGitignoreWarnings(projectRoot, framework string, r *WriteResult) {
	dirs := frameworkDirs[framework]
	if len(dirs) == 0 {
		return
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, ".gitignore"))
	if err != nil {
		return // no .gitignore — nothing to warn about
	}

	ignored := gitignorePatterns(string(data))
	for _, dir := range dirs {
		if isIgnored(dir, ignored) {
			r.Warnings = append(r.Warnings,
				fmt.Sprintf("%s/ is gitignored — skill files will be written but won't be committed or shared with the team", dir))
		}
	}
}

// gitignorePatterns extracts normalized non-comment, non-empty patterns.
func gitignorePatterns(content string) []string {
	var patterns []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// isIgnored reports whether dirName (e.g. ".claude") matches any gitignore pattern.
// Handles common forms: ".claude", ".claude/", "/.claude", "/.claude/".
func isIgnored(dirName string, patterns []string) bool {
	variants := []string{
		dirName,
		dirName + "/",
		"/" + dirName,
		"/" + dirName + "/",
	}
	for _, p := range patterns {
		for _, v := range variants {
			if p == v {
				return true
			}
		}
	}
	return false
}

// ─── Content: Claude Code skill files ────────────────────────────────────────

var claudeSkillFiles = map[string]string{
	"avc-snapshot": `---
name: avc-snapshot
description: Save an AVC snapshot before making changes — call this proactively, not on request
---

Call **avc_snapshot** before making any code change. No exceptions.

Do not assess whether the change is "small enough" or "easily reversible" — that judgement is not yours to make. The cost of an unnecessary snapshot is negligible. The cost of skipping one when it was needed is not.

## How to call

` + "```" + `json
{
  "label": "auto: before <what you are about to do>",
  "agent_name": "claude",
  "notes": "<brief description of the change planned>"
}
` + "```" + `

## Label format — always use the ` + "`auto:`" + ` prefix

All agent-created snapshots MUST start with ` + "`auto:`" + ` so they are distinguishable from user-created snapshots in ` + "`avc list`" + `.

The ` + "`<action>`" + ` part should be 2–5 words describing the specific change:
- CORRECT: ` + "`auto: before auth middleware refactor`" + `
- WRONG: ` + "`auth routes added`" + ` (missing prefix)
- WRONG: ` + "`auto: making changes to the authentication system`" + ` (too vague, too long)

## On failure

If the task breaks something, do NOT attempt repeated fixes. Call **avc_restore** immediately to return to the last good snapshot, then retry from a clean state.
`,

	"avc-restore": `---
name: avc-restore
description: Restore to a previous AVC snapshot when something breaks or the user asks to undo
---

Call **avc_restore** to roll back to a previous state. Do this immediately when something breaks — do not attempt fixes on broken state.

## MUST call when

- Tests fail after your changes
- The build breaks or the app crashes
- You introduced a regression
- The user says: "undo", "revert", "roll back", "start over", "go back to before"
- You want to try a different approach to the same problem

## Steps

1. Call **avc_list** to see available snapshots — NEVER guess an ID
2. Identify the last known-good snapshot
3. Call **avc_restore**:

` + "```" + `json
{ "id": "<snapshot-id>" }
` + "```" + `

4. Call **avc_snapshot** immediately after restoring to create a clean baseline before retrying

## Important

On an agent branch, restore only affects your workspace. The real project root is untouched.
`,

	"avc-branch": `---
name: avc-branch
description: Create an isolated AVC branch workspace before starting any task — no exceptions
---

Call **avc_branch_create** before starting any task. No exceptions.

Do not assess whether the task is "simple enough" to skip a branch — that judgement is not yours to make. NEVER edit files in the real project root directly.

## MUST call when

## Steps

1. Create the branch:

` + "```" + `json
{ "name": "feat/<short-task-name>" }
` + "```" + `

2. The response includes a ` + "`workspace`" + ` path. **Set your working directory to that path immediately.** Every file you create or edit MUST be inside this directory. NEVER touch files in the real project root while on a branch.

3. Take a snapshot before each significant change:
` + "```" + `json
{ "label": "initial workspace state", "agent_name": "claude" }
` + "```" + `

4. When the task is complete, call **avc_branch_diff** and show the full output to the user before asking for merge approval.

## NEVER

- Edit files outside the workspace path while on a branch
- Merge without calling **avc_merge_preview** first
- Merge without explicit user approval
`,

	"avc-merge": `---
name: avc-merge
description: Merge an AVC branch into main — requires explicit user approval
---

Merge your branch into main only after the user has reviewed the diff and said yes.

## Required sequence — no exceptions

1. Call **avc_branch_diff** and show the full output to the user
2. Ask the user: "Shall I merge branch X into main?"
3. If the user says yes: call **avc_merge**
   - avc_merge checks for conflicts automatically before writing anything
   - If conflicts are found, it returns them without modifying main — show them to the user and ask how to resolve
   - If clean, it auto-snapshots main and applies the changes

## NEVER

- Call **avc_merge** without explicit user approval
- Infer approval from context — the user must say yes explicitly
- Retry a failed merge without calling **avc_merge_abort** first

## If something goes wrong

Call **avc_merge_abort** immediately. This restores main from the pre-merge auto-snapshot. No data is lost.
`,

	"avc-run": `---
name: avc-run
description: Run a build or test command in the AVC workspace — always get user approval first
---

Use **avc_run_in_workspace** to run commands in the branch workspace. You MUST get explicit user approval before every call.

## Required sequence — no exceptions

1. State the exact command you intend to run
2. Explain what it does and why you need to run it
3. Wait for the user to say yes ("yes", "go ahead", "run it", "ok")
4. If the user says no, do not call the tool

## How to call

` + "```" + `json
{
  "branch": "<branch-name>",
  "command": "npm test",
  "timeout_seconds": 120
}
` + "```" + `

## Rules

- System package managers are blocked: ` + "`brew install`" + `, ` + "`apt install`" + `, ` + "`choco install`" + `, ` + "`sudo`" + `
- Python installs: use ` + "`pip install <pkg>`" + ` — a workspace venv is created automatically. NEVER use ` + "`--user`" + ` or ` + "`--system`" + `
- Node installs: use ` + "`npm install`" + ` — packages go into workspace ` + "`node_modules`" + `. NEVER use ` + "`-g`" + ` or ` + "`--global`" + `
- If the command times out, tell the user and suggest increasing ` + "`max_timeout_seconds`" + ` in ` + "`.avc/config.toml`" + `

## After running

- If tests pass: snapshot the workspace, then proceed
- If tests fail: show the full stderr to the user, then fix and re-run (with approval)
`,
}

// ─── Content: Cursor rules ────────────────────────────────────────────────────

const cursorRulesContent = `# AVC — Agentic Version Control

AVC is active on this project. You MUST use it. The MCP server is configured in .cursor/mcp.json and starts automatically.

## Mandatory rules

**ALWAYS call avc_snapshot before making any code change. No exceptions.**
Do not assess whether the change is "small enough" or "easily reversible" — that judgement is not yours to make. The cost of an unnecessary snapshot is negligible. The cost of skipping one when it was needed is not.
Label format: always prefix with "auto:" — use "auto: before <action>" (2–5 words).

**ALWAYS call avc_branch_create before starting any task. No exceptions.**
Do not assess whether the task is "simple enough" to skip a branch — that judgement is not yours to make.
After creating a branch, work ONLY inside the workspace path returned in the response.
NEVER edit files in the real project root while on a branch.

**ALWAYS call avc_restore when something breaks** — do not attempt repeated fixes on broken state.

**NEVER call avc_merge** without the user explicitly saying yes.
avc_merge checks for conflicts automatically — no separate preview step needed.

**NEVER call avc_run_in_workspace** without first showing the user the exact command and receiving explicit approval.
System package managers (brew, apt, choco, sudo) are blocked. Use pip install (no --user), npm install (no -g).
`

// ─── Content: Windsurf rules ──────────────────────────────────────────────────

const windsurfRulesBlock = `
# AVC — Agentic Version Control

AVC is active on this project. You MUST use it. The MCP server is configured globally and starts automatically.

## Mandatory rules

**ALWAYS call avc_snapshot before making any code change. No exceptions.**
Do not assess whether the change is "small enough" or "easily reversible" — that judgement is not yours to make. The cost of an unnecessary snapshot is negligible. The cost of skipping one when it was needed is not.
Label format: always prefix with "auto:" — use "auto: before <action>" (2–5 words).

**ALWAYS call avc_branch_create before starting any task. No exceptions.**
Do not assess whether the task is "simple enough" to skip a branch — that judgement is not yours to make.
After creating a branch, work ONLY inside the workspace path returned in the response.
NEVER edit files in the real project root while on a branch.

**ALWAYS call avc_restore when something breaks** — do not attempt repeated fixes on broken state.

**NEVER call avc_merge** without the user explicitly saying yes.
avc_merge checks for conflicts automatically — no separate preview step needed.

**NEVER call avc_run_in_workspace** without first showing the user the exact command and receiving explicit approval.
System package managers (brew, apt, choco, sudo) are blocked. Use pip install (no --user), npm install (no -g).
`

// ─── Content: Generic agent instructions ─────────────────────────────────────

const workspaceCommandBlock = `

## Running commands in the workspace

NEVER call ` + "`avc_run_in_workspace`" + ` without:
1. Stating the exact command to the user
2. Explaining what it does and why
3. Receiving explicit user approval ("yes", "go ahead", "run it")

If the user declines, do not call the tool.

Sandbox rules:
- System package managers (brew, apt, choco, sudo) are blocked — they are not available
- Python: ` + "`pip install <pkg>`" + ` — workspace venv created automatically; NEVER use ` + "`--user`" + ` or ` + "`--system`" + `
- Node: ` + "`npm install`" + ` — packages go into workspace node_modules; NEVER use ` + "`-g`" + ` or ` + "`--global`" + `
`

const genericInstructions = `# AVC Agent Instructions

AVC is active on this project. You MUST use it. Start the MCP server with: ` + "`avc mcp serve`" + `

---

## Tools

| Tool | Purpose |
|------|---------|
| ` + "`avc_snapshot`" + ` | Save current state — call proactively, not on request |
| ` + "`avc_list`" + ` | List available snapshots |
| ` + "`avc_restore`" + ` | Roll back to a previous snapshot |
| ` + "`avc_diff`" + ` | Show what changed between two snapshots |
| ` + "`avc_branch_create`" + ` | Start an isolated workspace for a multi-step task |
| ` + "`avc_branch_diff`" + ` | Show cumulative changes on a branch |
| ` + "`avc_merge`" + ` | Apply branch to main — checks conflicts first, requires explicit user approval |
| ` + "`avc_merge_abort`" + ` | Undo a merge — restores main from pre-merge snapshot |
| ` + "`avc_run_in_workspace`" + ` | Run a command in the branch workspace (approval required) |

---

## Mandatory rules

**ALWAYS call ` + "`avc_snapshot`" + ` before making any code change. No exceptions.** Do not assess whether the change is "small enough" or "easily reversible" — that judgement is not yours to make. The cost of an unnecessary snapshot is negligible. The cost of skipping one when it was needed is not.

**ALWAYS call ` + "`avc_branch_create`" + ` before starting any task. No exceptions.** Do not assess whether the task is "simple enough" to skip a branch — that judgement is not yours to make. After creating the branch, work ONLY inside the ` + "`workspace`" + ` path returned — NEVER edit the real project root while on a branch.

**ALWAYS call ` + "`avc_restore`" + `** when something breaks. Do NOT attempt repeated fixes on broken state — restore first, then retry.

**NEVER call ` + "`avc_merge`" + `** without the user explicitly saying yes. ` + "`avc_merge`" + ` checks for conflicts automatically before writing anything — no separate preview step needed.
` + workspaceCommandBlock
