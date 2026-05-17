---
title: Claude Code
description: Integrate AVC with Claude Code via MCP and skills.
sidebar:
  order: 2
---

[Claude Code](https://claude.ai/code) is Anthropic's official CLI for Claude. AVC integrates with it via the Model Context Protocol — Claude can call `avc_snapshot`, `avc_diff`, `avc_restore`, etc. as native tools.

## Setup

```bash
avc init --skills claude-code
```

That single command writes everything needed.

## What gets written

```
.claude/
├── settings.json                       ← MCP server registration
└── skills/
    ├── avc-snapshot/SKILL.md           ← "When and how to snapshot"
    ├── avc-branch/SKILL.md             ← "When to use branches"
    ├── avc-restore/SKILL.md            ← "When to roll back"
    └── avc-merge/SKILL.md              ← "How to merge an agent branch"
```

The `settings.json` registers the AVC MCP server:

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

The skill files are prompt instructions Claude reads when it loads the project. They explain the four primitives and when to use each tool.

## Workflow

Open the project in Claude Code:

```bash
claude
```

Claude detects the `.claude/` directory and loads the skills. Ask it to do something non-trivial:

> "Refactor the auth module to use bcrypt"

What Claude does (with AVC integration):

1. Calls `avc_branch_create` to make a `refactor-auth` workspace
2. Edits files inside `.avc/workspaces/refactor-auth/`
3. Calls `avc_snapshot` after each meaningful change
4. When done, summarizes what it did and asks if you want to merge

You can run `avc list` (or open the [Web UI](/agentic-vc/web-ui/)) at any time to see every snapshot Claude took.

## Updating skills

If you update AVC and want the latest skill files:

```bash
avc init --skills claude-code
```

This is idempotent — it overwrites `.claude/settings.json` and the AVC skill files but leaves your other skills alone.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Claude says "tool avc_snapshot not found" | Restart Claude Code so it re-reads `.claude/settings.json` |
| `avc: command not found` in the MCP error | Set the full path in `settings.json`: `"command": "/usr/local/bin/avc"` |
| Skills not being followed | Confirm `.claude/skills/avc-*/SKILL.md` files exist; rerun `avc init --skills claude-code` |
