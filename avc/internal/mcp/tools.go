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

// AllTools returns the full list of AVC MCP tools.
func AllTools() []Tool {
	return []Tool{
		{
			Name: "avc_snapshot",
			Description: "Save a snapshot of the current project state. " +
				"Call before making any significant changes, after completing a task, " +
				"or whenever you want a safe restore point. " +
				"When on an agent branch the snapshot captures the workspace, not the real project root.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"label":      {Type: "string", Description: "Short description of this snapshot (e.g. 'before refactor', 'added auth flow')"},
					"agent_name": {Type: "string", Description: "Name or identifier of the agent creating this snapshot"},
					"notes":      {Type: "string", Description: "Additional context about what changed or why"},
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
				"A workspace directory is materialized from the base snapshot — direct all your file edits there. " +
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
				"Use this to review everything the agent changed before requesting a merge.",
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
			Description: "Preview what a three-way merge of a branch into main would do — " +
				"which files would be applied cleanly, which would have conflicts, and which are unchanged. " +
				"Does NOT write any files.",
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
			Description: "Merge a branch into main using three-way merge. " +
				"AVC auto-snapshots main before writing, so the merge is fully reversible with avc_merge_abort. " +
				"Clean changes are applied automatically. Conflicts are written with <<<<<<< markers for resolution.",
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
	}
}
