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
			Name:        "avc_list",
			Description: "List all snapshots on the active branch, newest first.",
			InputSchema: InputSchema{Type: "object"},
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
	}
}
