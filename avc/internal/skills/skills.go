// Package skills writes MCP server configs and agent instruction files
// for the frameworks supported by `avc init --skills <framework>`.
package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Supported framework identifiers.
const (
	FrameworkClaudeCode = "claude-code"
	FrameworkCursor     = "cursor"
	FrameworkWindsurf   = "windsurf"
	FrameworkGeneric    = "generic"
)

// SupportedFrameworks lists all valid --skills values.
var SupportedFrameworks = []string{
	FrameworkClaudeCode,
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
	// MCP config — .claude/settings.json
	settingsPath := filepath.Join(projectRoot, ".claude", "settings.json")
	action, err := mergeMCPConfig(settingsPath, "avc")
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(".claude/settings.json", action))

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
	// MCP config — .cursor/mcp.json
	mcpPath := filepath.Join(projectRoot, ".cursor", "mcp.json")
	action, err := mergeMCPConfig(mcpPath, "avc")
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(".cursor/mcp.json", action))

	// Rules file — .cursor/rules/avc.mdc
	rulesPath := filepath.Join(projectRoot, ".cursor", "rules", "avc.mdc")
	action, err = writeFileIfAbsent(rulesPath, cursorRulesContent)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(".cursor/rules/avc.mdc", action))
	return nil
}

func writeWindsurf(projectRoot string, r *WriteResult) error {
	// MCP config — .codeium/windsurf/mcp_config.json
	mcpPath := filepath.Join(projectRoot, ".codeium", "windsurf", "mcp_config.json")
	action, err := mergeMCPConfig(mcpPath, "avc")
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(".codeium/windsurf/mcp_config.json", action))

	// Rules file — .windsurfrules (append, idempotent)
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

// ─── MCP config helpers ───────────────────────────────────────────────────────

var mcpServerEntry = map[string]any{
	"command": "avc",
	"args":    []string{"mcp", "serve"},
}

// mergeMCPConfig merges the AVC server entry into the JSON file at path.
// Returns "created", "updated", or "skipped:already configured".
func mergeMCPConfig(path, serverKey string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
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

	if _, exists := servers[serverKey]; exists {
		return "skipped:already configured", nil
	}

	servers[serverKey] = mcpServerEntry
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
func appendRulesBlock(path, block string) (string, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if strings.Contains(string(existing), avcRulesMarker) {
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

// frameworkDirs maps each framework to the top-level directories it writes into.
var frameworkDirs = map[string][]string{
	FrameworkClaudeCode: {".claude"},
	FrameworkCursor:     {".cursor"},
	FrameworkWindsurf:   {".codeium"},
	FrameworkGeneric:    {}, // writes to project root — no subdirectory to check
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
description: Save a snapshot of the current project state with AVC
---

Save a snapshot of the current project state using the **avc_snapshot** MCP tool.

## When to use

- Before making any significant code change
- After completing a task or subtask successfully
- Before running a risky operation (refactor, dependency upgrade, schema migration)
- Whenever the user asks you to "save" or "checkpoint" your work

## How to use

Call **avc_snapshot** with a descriptive label:

` + "```" + `json
{
  "label": "<short past-tense description of current state>",
  "agent_name": "claude",
  "notes": "<optional: what you are about to do or what just changed>"
}
` + "```" + `

## Tips

- Labels should describe current state, not intentions: "auth middleware added" not "adding auth"
- Always snapshot before a multi-file refactor
- If a task fails, use **avc_restore** to roll back to the last good snapshot
`,

	"avc-restore": `---
name: avc-restore
description: Restore the project to a previous AVC snapshot
---

Restore the project to a previous snapshot using **avc_restore**.

## When to use

- After a failed attempt — tests broken, build won't compile, logic regressed
- When the user asks you to "undo", "roll back", or "start over"
- Before trying a different approach to the same problem

## How to use

1. List available snapshots with **avc_list**
2. Pick the snapshot ID to restore to
3. Call **avc_restore**:

` + "```" + `json
{ "id": "<snapshot-id>" }
` + "```" + `

## Tips

- Always list snapshots first — never guess an ID
- After restoring, take a new snapshot before trying again so you have a clean baseline
- On an agent branch, restore only affects your workspace — main is untouched
`,

	"avc-branch": `---
name: avc-branch
description: Create an isolated agent branch workspace in AVC
---

Create an isolated branch workspace so your changes don't affect the main project
until the user approves them.

## When to use

- When starting a non-trivial task that might need review before going live
- When the user wants to see a diff of all changes before they're applied
- When experimenting with an approach you are not sure about

## Workflow

1. Create a branch — this materializes a copy of files in a workspace directory:

` + "```" + `json
{ "name": "feature/my-task" }
` + "```" + `

2. **Work only inside the workspace path** returned in the response. Do not edit files in the real project root.
3. Snapshot regularly inside the workspace with **avc_snapshot**.
4. When done, call **avc_branch_diff** to show the user what changed.
5. Ask for approval, then call **avc_merge** to apply changes to main.
`,

	"avc-merge": `---
name: avc-merge
description: Merge an agent branch into main with AVC (requires user approval)
---

Merge your agent branch back into main after the user approves your work.

## When to use

- After the user reviews **avc_branch_diff** output and explicitly approves
- Never merge without explicit user approval

## Workflow

1. Preview first — no files are written:

` + "```" + `json
{ "branch": "<branch-name>" }
` + "```" + `

2. Show the preview to the user (clean / conflict / skipped counts).
3. If the user approves, call **avc_merge** with the same branch name.
4. If conflicts are reported, list them and ask the user how to resolve.
5. After resolution, take a new snapshot on main to record the final state.

## Abort

If anything goes wrong, **avc_merge_abort** restores main from the pre-merge snapshot.
`,
}

// ─── Content: Cursor rules ────────────────────────────────────────────────────

const cursorRulesContent = `# AVC — Agentic Version Control

AVC is configured as an MCP server in .cursor/mcp.json.

## Rules

- Call **avc_snapshot** before any significant code change and after completing each task.
- For non-trivial tasks, create an agent branch with **avc_branch_create** so changes can be reviewed before going live. Work only inside the returned workspace path.
- If a task fails or produces broken code, call **avc_restore** to roll back cleanly.
- Before merging, always call **avc_merge_preview** first and present the result to the user.
- Never call **avc_merge** without explicit user approval.
`

// ─── Content: Windsurf rules ──────────────────────────────────────────────────

const windsurfRulesBlock = `
# AVC — Agentic Version Control

AVC is configured as an MCP server in .codeium/windsurf/mcp_config.json.

## Rules

- Call **avc_snapshot** before any significant code change and after completing each task.
- For non-trivial tasks, create an agent branch with **avc_branch_create** so changes can be reviewed before going live. Work only inside the returned workspace path.
- If a task fails or produces broken code, call **avc_restore** to roll back cleanly.
- Before merging, always call **avc_merge_preview** first and present the result to the user.
- Never call **avc_merge** without explicit user approval.
`

// ─── Content: Generic agent instructions ─────────────────────────────────────

const genericInstructions = `# AVC Agent Instructions

This project uses **AVC (Agentic Version Control)** — a lightweight snapshot system
designed for agent-assisted development. AVC is available via MCP server.

Start the server with: ` + "`avc mcp serve`" + `

---

## Core tools

| Tool | When to call |
|------|-------------|
| ` + "`avc_snapshot`" + ` | Before any significant change; after completing a task |
| ` + "`avc_list`" + ` | To review available restore points |
| ` + "`avc_restore`" + ` | When a task fails; when the user asks to undo |
| ` + "`avc_diff`" + ` | To show the user what changed between two snapshots |
| ` + "`avc_branch_create`" + ` | To start a non-trivial task in an isolated workspace |
| ` + "`avc_branch_diff`" + ` | To show the user a cumulative diff before merging |
| ` + "`avc_merge_preview`" + ` | To preview a merge without writing files |
| ` + "`avc_merge`" + ` | To apply branch changes to main (requires user approval) |
| ` + "`avc_merge_abort`" + ` | To undo a merge gone wrong |

---

## Rules

1. **Snapshot before risk.** Call ` + "`avc_snapshot`" + ` before any refactor, migration, or multi-file change.
2. **Branch for non-trivial tasks.** Use ` + "`avc_branch_create`" + ` so changes can be reviewed before going live. Work exclusively inside the workspace path returned by that tool.
3. **Restore on failure.** If a task produces broken code, call ` + "`avc_restore`" + ` instead of attempting repeated fixes on broken state.
4. **Preview before merge.** Always call ` + "`avc_merge_preview`" + ` and show the user the result before merging.
5. **Never merge without approval.** The user must explicitly approve before you call ` + "`avc_merge`" + `.
`
