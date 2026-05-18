---
title: Windsurf
description: Integrate AVC with Windsurf via MCP and Codeium rules.
sidebar:
  order: 4
---

[Windsurf](https://codeium.com/windsurf) is Codeium's agent-first IDE. AVC integrates via Codeium's MCP server convention and `.windsurfrules`.

## Setup

```bash
avc init --skills windsurf
```

## What gets written

```
.codeium/windsurf/
└── mcp_config.json       ← MCP server registration
.windsurfrules            ← AVC section appended (other rules preserved)
```

The MCP config:

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

The `.windsurfrules` file gets an AVC section appended:

```
# AVC

Use the AVC MCP tools for version control:
- avc_snapshot before any non-trivial change
- avc_branch_create for refactors / new features
- avc_merge when work is ready (after preview)
- avc_restore to undo a bad change

Always work inside the workspace path returned by avc_branch_create.
```

Other rules in `.windsurfrules` are preserved — AVC only adds or updates its own block (marked by a `# AVC` header).

## Workflow

Open the project in Windsurf. The Cascade agent picks up the MCP config automatically.

Ask Cascade something:

> "Add a unit test for the payment handler"

Cascade (with AVC integration) snapshots, optionally branches, makes changes, and snapshots again. You see every step in the AVC snapshot list.

## Verifying it's working

In Cascade's chat, ask:

> "what MCP tools do you have?"

Look for 14 tools starting with `avc_`. If they're missing, restart Windsurf.

## Updating

```bash
avc init --skills windsurf
```

Rewrites the MCP config and the AVC section of `.windsurfrules`.
