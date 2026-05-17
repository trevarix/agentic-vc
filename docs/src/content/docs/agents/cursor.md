---
title: Cursor
description: Integrate AVC with Cursor via MCP and rules.
sidebar:
  order: 3
---

[Cursor](https://cursor.sh/) supports MCP servers and per-project rules. AVC plugs into both.

## Setup

```bash
avc init --skills cursor
```

## What gets written

```
.cursor/
├── mcp.json              ← MCP server registration
└── rules/
    └── avc.mdc           ← Instructions on when to use AVC tools
```

The `mcp.json`:

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

The `avc.mdc` rules file instructs Cursor on the AVC workflow — branching for non-trivial work, snapshotting before risky changes, etc.

## Workflow

Open the project in Cursor. The MCP server is loaded automatically. When you ask Cursor to do something via Chat or Composer, it can call AVC tools as part of its actions.

Example prompt:

> "Add bcrypt to the auth module"

Cursor (with AVC integration) will:

1. Snapshot the current state (`avc_snapshot`)
2. Create a branch (`avc_branch_create`)
3. Make changes inside the workspace
4. Snapshot the result
5. Optionally preview a merge

## Verifying it's working

In Cursor's Chat panel, type:

> "list all AVC tools you have"

If integration is working, Cursor will list 14 tools starting with `avc_`. If not, restart Cursor and check `.cursor/mcp.json` exists.

## Updating

```bash
avc init --skills cursor
```

Rewrites `.cursor/mcp.json` and `.cursor/rules/avc.mdc`. Existing rules files for other tools are left alone.
