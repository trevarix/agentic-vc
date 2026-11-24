// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package skills

import (
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/mcp"
)

// defaultToolTier is the MCP tool tier written into every generated server
// config. The agent rules are rendered for this same tier, so they never tell
// an agent to call a tool the server does not advertise.
const defaultToolTier = "standard"

// runToolName is the one rule-bearing tool that is not in every tier.
const runToolName = "avc_run_in_workspace"

// rulesTitle is the heading every AVC rules section and file carries. Older
// AVC versions used it as the only marker for the Windsurf rules block, so it
// doubles as a legacy marker.
const rulesTitle = "AVC — Agentic Version Control"

// blockStartMarker and blockEndMarker delimit the AVC section in a file that
// also holds other content (CLAUDE.md, AGENTS.md). Everything between them is
// AVC's and is rewritten when the rules change.
const (
	blockStartMarker = "<!-- " + rulesTitle + " -->"
	blockEndMarker   = "<!-- /" + rulesTitle + " -->"
)

// managedFileMarker marks a whole file as written and owned by AVC, so a later
// `avc init --skills` may rewrite it with current rules. A file without it is
// treated as the user's and never overwritten.
const managedFileMarker = "<!-- AVC-managed file: rewritten by `avc init --skills`. Put your own rules in another file. -->"

// tierHasTool reports whether the named MCP tier advertises tool.
func tierHasTool(tier, tool string) bool {
	for _, t := range mcp.ToolsForTier(tier) {
		if t.Name == tool {
			return true
		}
	}
	return false
}

// rulesBody renders the agent rules shared by every framework, for the given
// MCP tool tier. Each framework wraps it with its own header (see the render
// functions below); the rules themselves exist only here, so they cannot drift
// apart between tools.
func rulesBody(tier string) string {
	hasRun := tierHasTool(tier, runToolName)

	var b strings.Builder
	b.WriteString(`### Mandatory actions

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
`)
	if hasRun {
		b.WriteString(`
**NEVER call ` + "`avc_run_in_workspace`" + `** without first:
1. Showing the user the exact command you intend to run
2. Explaining what it does
3. Receiving explicit approval

System package managers (brew, apt, choco, sudo) are blocked. Python: ` + "`pip install`" + ` (no ` + "`--user`" + `). Node: ` + "`npm install`" + ` (no ` + "`-g`" + `).
`)
	} else {
		b.WriteString(`
Run builds and tests with your own terminal tool, inside the branch ` + "`workspace`" + ` directory.
`)
	}

	b.WriteString(`
### Quick reference

| Trigger | Tool |
|---------|------|
| About to make changes | ` + "`avc_snapshot`" + ` |
| Multi-step or multi-file task | ` + "`avc_branch_create`" + ` |
| Something broke | ` + "`avc_restore`" + ` |
| Ready to review branch work | ` + "`avc_branch_diff`" + ` |
| Ready to merge (with approval) | ` + "`avc_merge`" + ` |
`)
	if hasRun {
		b.WriteString("| Need to run tests or build | `avc_run_in_workspace` (approval required) |\n")
	}
	return b.String()
}

// rulesIntro is the sentence that opens every rules section: AVC is on, and
// where the agent's MCP server is configured.
func rulesIntro(serverLocation string) string {
	return "AVC is active on this project. You MUST use it. The `avc` MCP server is " +
		serverLocation + " and exposes `avc_*` tools.\n"
}

// renderBlock renders the AVC rules as a delimited section for a file that
// may hold other content (CLAUDE.md, AGENTS.md).
func renderBlock(serverLocation, tier string) string {
	return blockStartMarker + "\n## " + rulesTitle + "\n\n" +
		rulesIntro(serverLocation) + "\n" + rulesBody(tier) + blockEndMarker + "\n"
}

// renderManagedFile renders the AVC rules as a whole file AVC owns. frontmatter
// is the framework's YAML header (without the --- fences), or "" for none.
func renderManagedFile(frontmatter, serverLocation, tier string) string {
	var b strings.Builder
	if frontmatter != "" {
		b.WriteString("---\n" + frontmatter + "---\n\n")
	}
	b.WriteString(managedFileMarker + "\n\n# " + rulesTitle + "\n\n")
	b.WriteString(rulesIntro(serverLocation) + "\n" + rulesBody(tier))
	return b.String()
}
