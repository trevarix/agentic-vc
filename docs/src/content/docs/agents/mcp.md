---
title: MCP Server
description: How AVC exposes its commands as MCP tools for AI agents.
sidebar:
  order: 1
---

AVC includes a built-in [Model Context Protocol](https://modelcontextprotocol.io/) server. Any agent that speaks MCP can call AVC's commands as tools — no separate process to manage, no HTTP layer to configure.

## Start the server

```bash
avc mcp serve              # default pretty-printed output
avc mcp serve --compact    # compact JSON for token-sensitive contexts
```

The server runs over stdio: it reads JSON-RPC 2.0 messages on stdin and writes responses on stdout. Agent frameworks spawn it as a subprocess.

## Available tools

| Tool | Maps to |
|------|---------|
| `avc_snapshot` | [`avc snapshot`](/agentic-vc/cli/snapshot/) |
| `avc_list` | [`avc list`](/agentic-vc/cli/list/) |
| `avc_info` | [`avc info`](/agentic-vc/cli/info/) |
| `avc_delete` | [`avc delete`](/agentic-vc/cli/delete/) |
| `avc_diff` | [`avc diff`](/agentic-vc/cli/diff/) |
| `avc_restore` | [`avc restore`](/agentic-vc/cli/restore/) |
| `avc_branch_create` | [`avc branch create`](/agentic-vc/cli/branch/) |
| `avc_branch_list` | [`avc branch list`](/agentic-vc/cli/branch/) |
| `avc_branch_switch` | [`avc branch switch`](/agentic-vc/cli/branch/) |
| `avc_branch_diff` | [`avc branch diff`](/agentic-vc/cli/branch/) |
| `avc_merge_preview` | [`avc merge --preview`](/agentic-vc/cli/merge/) |
| `avc_merge` | [`avc merge`](/agentic-vc/cli/merge/) |
| `avc_merge_abort` | [`avc merge --abort`](/agentic-vc/cli/merge/) |
| `avc_run_in_workspace` | Run a shell command inside a branch workspace |

Each tool's JSON Schema is published via `tools/list` so the agent can discover them programmatically.

## Don't configure it by hand

Use the one-shot setup instead:

```bash
avc init --skills claude-code,cursor,windsurf,generic
```

This writes the right config files for each framework into your project. See the per-framework guides:

- [Claude Code](/agentic-vc/agents/claude-code/)
- [Cursor](/agentic-vc/agents/cursor/)
- [Windsurf](/agentic-vc/agents/windsurf/)

## Manual integration

For custom agents not covered by `--skills`, here's the minimal config snippet:

```json
{
  "mcpServers": {
    "avc": {
      "command": "avc",
      "args": ["mcp", "serve", "--compact"]
    }
  }
}
```

Drop that into whatever MCP config file your framework uses. Most frameworks (Claude Desktop, Cline, etc.) follow this convention.

## When the agent should use each tool

AVC ships **agent skill files** (e.g., `.claude/skills/avc-snapshot/SKILL.md`) that document when to call which tool. These are written by `avc init --skills` and serve as instructions to the agent — "always snapshot before risky changes", "use branches for non-trivial refactors", etc.

If you write your own agent, you can copy the skill files as a starting point for your prompt engineering.
