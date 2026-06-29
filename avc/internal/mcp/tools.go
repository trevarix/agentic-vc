// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

// Tool is a single MCP tool definition as returned by tools/list.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema is a minimal JSON Schema object describing a tool's arguments.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

// Property describes one field in a tool's input schema.
type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ProjectlessTools returns the tool set advertised when no AVC project is
// detected. Empty — no AVC tools are exposed so the agent cannot misuse them
// on an uninitialized directory. The user must run `avc init` from a terminal
// and restart Claude Code to enable AVC.
func ProjectlessTools() []Tool {
	return []Tool{}
}

// ToolsForTier returns the tool set for the named tier.
//
//	core     — 4 tools: snapshot, list, diff, restore
//	standard — 10 tools: core + branch_create/list/switch/diff + merge + merge_abort  (default)
//	full     — all tools (~24)
//
// An unrecognised tier name falls back to "standard".
func ToolsForTier(tier string) []Tool {
	switch tier {
	case "core":
		return CoreTools()
	case "full":
		return AllTools()
	default: // "standard" and anything unrecognised
		return StandardTools()
	}
}

// CoreTools returns the minimal 4-tool set: snapshot, list, diff, restore.
// Suitable for agents with small context windows that only need basic operations.
func CoreTools() []Tool {
	all := AllTools()
	coreNames := map[string]bool{
		"avc_snapshot": true,
		"avc_list":     true,
		"avc_diff":     true,
		"avc_restore":  true,
	}
	var out []Tool
	for _, t := range all {
		if coreNames[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

// StandardTools returns the 10-tool default set: core + branch create/list/switch/diff
// + merge + merge_abort. Covers the full agent workflow without advanced tools.
func StandardTools() []Tool {
	all := AllTools()
	standardNames := map[string]bool{
		"avc_snapshot":       true,
		"avc_list":           true,
		"avc_diff":           true,
		"avc_restore":        true,
		"avc_status":         true,
		"avc_branch_create":  true,
		"avc_branch_list":    true,
		"avc_branch_switch":  true,
		"avc_branch_diff":    true,
		"avc_merge":          true,
		"avc_merge_abort":    true,
	}
	var out []Tool
	for _, t := range all {
		if standardNames[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

// AllTools returns the full list of AVC MCP tools.
func AllTools() []Tool {
	return []Tool{
		{
			Name: "avc_snapshot",
			Description: "Save a snapshot of the current project state. " +
				"Call after avc_branch_create (before touching any files) and after each completed subtask. " +
				"When on an agent branch the snapshot captures the workspace, not the real project root. " +
				"Always provide agent_name and notes. " +
				"The response field 'file_count' is the total number of tracked files — not a delta. " +
				"Compare 'total_size' between snapshots to confirm changes were captured.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"label":      {Type: "string", Description: "Required. Prefix with 'auto:' — e.g. 'auto: before auth refactor' or 'auto: after test fixes'"},
					"agent_name": {Type: "string", Description: "Name of the agent creating this snapshot, e.g. 'claude'. Defaults to 'agent' if omitted."},
					"notes":      {Type: "string", Description: "Brief description of what you are about to do or just completed"},
				},
				Required: []string{"label"},
			},
		},
		{
			Name: "avc_list",
			Description: "List snapshots on the active branch, newest first. " +
				"Supports optional filters: tag (only snapshots with that tag), " +
				"search (full-text on label/notes), agent (filter by agent name), " +
				"changed (only snapshots that tracked a specific file path), " +
				"limit (max results; default 50, -1 = unlimited).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"tag":     {Type: "string", Description: "Only snapshots with this tag (e.g. 'stable', 'v1.0.0')"},
					"search":  {Type: "string", Description: "Full-text search on label and notes"},
					"agent":   {Type: "string", Description: "Filter by agent name (substring match)"},
					"changed": {Type: "string", Description: "Only snapshots that tracked this relative file path"},
					"limit":   {Type: "integer", Description: "Max results (default 50; -1 = unlimited)"},
					"all":     {Type: "boolean", Description: "Show snapshots from all branches (default: active branch only)"},
				},
			},
		},
		{
			Name: "avc_diff",
			Description: "Show the file-level diff between two snapshots. " +
				"Returns added, modified, and deleted files with line counts and unified diff previews.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"from_id": {Type: "string", Description: "ID of the earlier snapshot"},
					"to_id":   {Type: "string", Description: "ID of the later snapshot"},
				},
				Required: []string{"from_id", "to_id"},
			},
		},
		{
			Name: "avc_restore",
			Description: "Restore the project to a previous snapshot. " +
				"On an agent branch this restores the workspace only — the real project root is untouched. " +
				"Use this to undo mistakes without touching main.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {Type: "string", Description: "Snapshot ID to restore to"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name: "avc_info",
			Description: "Get detailed information about a snapshot, including the full list of tracked files.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {Type: "string", Description: "Snapshot ID"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name: "avc_delete",
			Description: "Permanently delete a snapshot and its file records. Cannot be undone.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {Type: "string", Description: "Snapshot ID to delete"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name: "avc_branch_create",
			Description: "Create a new agent branch and switch to it. " +
				"Call this FIRST before any other action — before avc_snapshot, before reading or writing any files. " +
				"The response contains a 'workspace' path: use that path for ALL subsequent file reads and writes. " +
				"NEVER edit files in the real project root while on a branch. " +
				"The real project root remains untouched until the user approves a merge.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name":             {Type: "string", Description: "Branch name (e.g. 'feature/add-auth', 'fix/payment-bug')"},
					"from_snapshot_id": {Type: "string", Description: "Base snapshot ID. Defaults to HEAD of main if omitted."},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "avc_branch_list",
			Description: "List all branches. The active branch is marked. Includes workspace path for each non-main branch.",
			InputSchema: InputSchema{Type: "object"},
		},
		{
			Name: "avc_branch_switch",
			Description: "Switch the active branch. Does not modify any files — use avc_restore to roll the workspace to a specific snapshot.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string", Description: "Branch name to switch to"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name: "avc_branch_diff",
			Description: "Show the cumulative diff from a branch's base snapshot to its HEAD. " +
				"Call this at task completion and display the full output to the user. " +
				"After showing the diff, explain what changed and why, then tell the user: " +
				"'If you approve, I will run avc_merge_preview (a dry run) and show you the result before anything is written.'",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string", Description: "Branch name"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name: "avc_branch_rename",
			Description: "Rename a branch. Updates the DB record, workspace directory, and active branch reference. " +
				"Cannot rename 'main'.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"old_name": {Type: "string", Description: "Current branch name"},
					"new_name": {Type: "string", Description: "New branch name"},
				},
				Required: []string{"old_name", "new_name"},
			},
		},
		{
			Name: "avc_branch_abandon",
			Description: "Mark a branch as abandoned. No data is removed — snapshots and workspace remain. " +
				"The branch is excluded from the default listing. Use when a line of work is no longer needed.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string", Description: "Branch name to abandon"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name: "avc_branch_prune_merged",
			Description: "Remove workspace directories for all branches with status 'merged'. " +
				"DB records and snapshots are preserved. Frees disk space after a clean merge workflow.",
			InputSchema: InputSchema{Type: "object"},
		},
		{
			Name: "avc_merge_preview",
			Description: "Optional dry-run preview of what a merge would do — " +
				"which files would be applied cleanly, which would conflict, and how many are unchanged. " +
				"Does NOT write any files. Not required before avc_merge — use it only if the user explicitly asks to preview first.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"branch": {Type: "string", Description: "Branch name to preview merging into main"},
				},
				Required: []string{"branch"},
			},
		},
		{
			Name: "avc_merge",
			Description: "Merge a branch into main. " +
				"Automatically checks for conflicts first — if any are found, returns them without writing anything. " +
				"If clean, auto-snapshots main then applies changes. Fully reversible with avc_merge_abort. " +
				"Call this directly after user approval — no separate preview step needed.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"branch": {Type: "string", Description: "Branch name to merge into main"},
				},
				Required: []string{"branch"},
			},
		},
		{
			Name:        "avc_merge_abort",
			Description: "Abort the last in-progress or conflicted merge. Restores main from the pre-merge auto-snapshot.",
			InputSchema: InputSchema{Type: "object"},
		},
		{
			Name: "avc_run_in_workspace",
			Description: "Execute a shell command inside an agent branch workspace. " +
				"The command runs with environment scrubbing (no host credentials), " +
				"an execution timeout, and process tree kill on timeout. " +
				"Python pip installs are redirected into a workspace-local venv automatically. " +
				"Node packages install into the workspace node_modules. " +
				"System package managers (brew, apt, choco, sudo) are blocked. " +
				"\n\nREQUIRES [run] enabled = true in .avc/config.toml — this must be set " +
				"manually by a human. Agents cannot enable it. " +
				"\n\nIMPORTANT: Always present the full command to the user and obtain " +
				"explicit approval before calling this tool. Never call it autonomously.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"branch":          {Type: "string", Description: "Branch name (must not be 'main')"},
					"command":         {Type: "string", Description: "Shell command to run in the workspace"},
					"timeout_seconds": {Type: "integer", Description: "Timeout in seconds (default 180, max 600). Overrides config."},
				},
				Required: []string{"branch", "command"},
			},
		},
		{
			Name: "avc_status",
			Description: "Show files changed since the last snapshot on the active branch. " +
				"Use this before avc_snapshot to confirm which files will be captured. " +
				"Returns an empty list when the working tree matches the last snapshot exactly. " +
				"On an agent branch this compares the workspace against its last snapshot.",
			InputSchema: InputSchema{Type: "object"},
		},
		{
			Name: "avc_restore_file",
			Description: "Restore a single file from a snapshot without affecting any other files. " +
				"On an agent branch this writes to the workspace only — the real project root is untouched. " +
				"Use this instead of avc_restore when you only need to undo one file's changes.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"snapshot_id": {Type: "string", Description: "Snapshot to restore the file from"},
					"path":        {Type: "string", Description: "Relative file path (e.g. 'src/auth.go')"},
				},
				Required: []string{"snapshot_id", "path"},
			},
		},
		{
			Name: "avc_annotate",
			Description: "Show which snapshot introduced each line of a file. " +
				"Returns [{line, snapshot_id, label, agent_name, timestamp}] ordered by line number. " +
				"Lines modified on disk but not yet snapshotted show the most recent snapshot. " +
				"Useful for tracing when a regression was introduced: " +
				"'Which snapshot added line 42 of auth.go?'",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"path": {Type: "string", Description: "Relative file path (e.g. 'src/auth.go')"},
				},
				Required: []string{"path"},
			},
		},
		{
			Name: "avc_tag_snapshot",
			Description: "Apply a tag to a snapshot. Tags are machine-readable milestone markers " +
				"(e.g. 'stable', 'v1.2.0', 'pre-release'). Applying the same tag twice is a no-op. " +
				"Use avc_list with tag= to retrieve tagged snapshots.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"snapshot_id": {Type: "string", Description: "Snapshot ID to tag"},
					"tag":         {Type: "string", Description: "Tag string (e.g. 'stable', 'v1.0.0')"},
				},
				Required: []string{"snapshot_id", "tag"},
			},
		},
		{
			Name: "avc_untag_snapshot",
			Description: "Remove a tag from a snapshot. No-op if the tag was not set.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"snapshot_id": {Type: "string", Description: "Snapshot ID to untag"},
					"tag":         {Type: "string", Description: "Tag string to remove"},
				},
				Required: []string{"snapshot_id", "tag"},
			},
		},
		{
			Name: "avc_list_conflicts",
			Description: "List all files with unresolved merge conflict markers in the project root. " +
				"Call after avc_merge reports conflicts to see exactly which files need resolution. " +
				"Returns a list of relative file paths that still contain <<<<<<< markers. " +
				"Use avc_resolve_conflict to resolve each one, then call avc_merge again.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"branch": {Type: "string", Description: "Branch name whose merge produced the conflicts"},
				},
				Required: []string{"branch"},
			},
		},
		{
			Name: "avc_resolve_conflict",
			Description: "Resolve a conflict in one file by choosing a version or providing resolved content. " +
				"After resolving all conflicts, call avc_snapshot to record the resolution, " +
				"then call avc_merge again to complete the merge. " +
				"resolution must be 'ours' (keep main), 'theirs' (keep branch), or 'content' (provide text).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"branch":     {Type: "string", Description: "Branch name whose merge produced the conflict"},
					"path":       {Type: "string", Description: "Relative file path to resolve (e.g. 'src/auth.go')"},
					"resolution": {Type: "string", Description: "'ours' (keep main), 'theirs' (keep branch), or 'content' (provide resolved text)"},
					"content":    {Type: "string", Description: "Resolved file content — only used when resolution is 'content'"},
				},
				Required: []string{"branch", "path", "resolution"},
			},
		},
	}
}
