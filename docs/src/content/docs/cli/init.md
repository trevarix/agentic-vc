---
title: avc init
description: Initialize AVC for a project.
---

Initialize AVC for a project. Creates `.avc/` with a SQLite database, default config, and `.avcignore`.

## Usage

```bash
avc init                            # initialize current directory
avc init /path/to/project           # initialize a specific directory
avc init --skills claude-code       # also write agent integration files
avc init --skills claude-code,cursor,windsurf
```

## Flags

| Flag | Description |
|------|-------------|
| `--skills <list>` | Comma-separated agent frameworks to set up. Supported: `claude-code`, `cursor`, `windsurf`, `generic` |
| `--json` | JSON output |

## JSON output

```json
{
  "id": "proj-a1b2c3",
  "path": "/path/to/project",
  "name": "project",
  "success": true
}
```

## What it creates

```
.avc/
├── avc.db
├── config.toml
├── .gitignore
└── objects/
.avcignore                    ← in the project root
```

Re-running on an already-initialized project is a no-op — no data loss.

## With `--skills`

For each requested framework, AVC writes the right files:

| Framework | Files written |
|-----------|--------------|
| `claude-code` | `.claude/settings.json` (MCP config), `.claude/skills/avc-*/SKILL.md` (skill files) |
| `cursor` | `.cursor/mcp.json`, `.cursor/rules/avc.mdc` |
| `windsurf` | `.codeium/windsurf/mcp_config.json`, appends to `.windsurfrules` |
| `generic` | `AGENT_INSTRUCTIONS.md` |

These tell the agent framework how to invoke AVC's MCP server and when to use each tool. See [Agent Integration](/agentic-vc/agents/) for details.
