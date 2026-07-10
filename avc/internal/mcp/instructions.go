package mcp

import "strings"

// ─── MCP instruction sections ─────────────────────────────────────────────────
// Each constant is one self-contained section of the agent instructions.
// Edit sections independently; buildInstructions() composes them in order.

const instrPreamble = `You are working in a project that uses AVC (Agentic Version Control). Follow these rules when making file changes.`

// instrWhenToUse gates AVC usage — agents must skip AVC for read-only tasks.
const instrWhenToUse = `WHEN TO USE AVC
Only use AVC tools when the task requires writing or modifying files. Skip AVC entirely for:
- Answering questions, explaining code, or reviewing without changes
- Read-only analysis (searching, reading, summarising)
- Any task where you will not write to the filesystem
If the task has no file writes, proceed normally without calling any avc_* tools.`

// instrNotInitialized tells the agent what to do when AVC is not set up.
const instrNotInitialized = `NOT INITIALIZED
If no avc_* tools appear in your tool list, AVC is not initialized in this project. Do NOT attempt to call any avc_* tool. If the task involves file changes, tell the user: "AVC is not initialized in this project. To enable it, run 'avc init' in the project directory from a terminal, then restart Claude Code." Then complete the task without AVC.`

// instrRequiredSequence is the core start/finish protocol for every file-editing task.
const instrRequiredSequence = `REQUIRED SEQUENCE — follow this order for every file-editing task:
START:
1. Call avc_branch_create to get an isolated workspace.
2. Read the "workspace" path from the response. ALL file reads and writes go to that path — never the original project root.
3. Always Read files from the workspace path before editing — do not reuse content read from the project root.
4. Call avc_snapshot (label: "auto: before <action>") before touching any files.
Then make your changes.
FINISH:
5. Call avc_snapshot to capture your edits.
6. Call avc_branch_diff and show the full output to the user.
7. Ask the user: "Shall I merge this into main?" — wait for explicit yes.
8. Call avc_merge. It checks for conflicts automatically; if any are found it returns them without writing anything.`

const instrBranches = `BRANCHES
- Call avc_branch_create before writing any files. No exceptions.
- The response contains "workspace": use that exact path for every file operation. NEVER touch the real project root.`

const instrSnapshots = `SNAPSHOTS
- Call avc_snapshot before making any code change. No exceptions.
- Call avc_snapshot again after finishing your edits, before calling avc_branch_diff — the diff compares snapshots, so unsaved edits won't appear until captured.
- Label format: "auto: before <action>" or "auto: after <action>" (2–5 words). Example: "auto: before auth refactor".
- Always provide agent_name (e.g. "claude") and notes describing the planned change.`

const instrRestore = `RESTORE
- Call avc_restore immediately when tests fail, the build breaks, or the user says "undo" or "roll back".
- Do NOT attempt repeated fixes on broken state — restore first, then retry.`

const instrMerge = `MERGE
- NEVER call avc_merge without the user explicitly saying yes.
- avc_merge checks for conflicts automatically before writing anything — no separate preview step needed.
- If the merge response contains an "error" field, conflicts were detected. Show them to the user and ask how to resolve before retrying.
- If anything goes wrong, call avc_merge_abort immediately.`

const instrRunningCommands = `RUNNING COMMANDS
- NEVER call avc_run_in_workspace without first stating the exact command to the user, explaining what it does, and receiving explicit approval.
- System package managers (brew, apt, choco, sudo) are blocked.
- Python: use pip install (no --user). Node: use npm install (no -g or --global).`

const instrProtectedPaths = `PROTECTED PATHS
- The project may list protected paths under [protect] in .avc/config.toml — files you must not change (CI workflows, secrets, build config).
- If a snapshot, status, or branch-diff response includes "protected_changes", tell the user immediately — do not wait until merge time.
- A merge that changes protected paths is refused mechanically. You cannot override this; only a human can, by running avc merge --allow-protected from a terminal. Never suggest editing [protect] to get around it.
- Protection applies to merges into main. It is one more reason to always work in a branch workspace, never in the project root.`

// buildInstructions composes all sections into the final MCP instructions string
// returned in the initialize response. Sections are injected into the agent's
// context automatically by MCP-capable clients (Claude Code, Cursor, Windsurf).
func buildInstructions() string {
	return strings.Join([]string{
		instrPreamble,
		instrWhenToUse,
		instrNotInitialized,
		instrRequiredSequence,
		instrBranches,
		instrSnapshots,
		instrRestore,
		instrMerge,
		instrRunningCommands,
		instrProtectedPaths,
	}, "\n\n")
}
