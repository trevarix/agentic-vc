---
title: CLI Overview
description: Every AVC command, organized by topic.
sidebar:
  order: 0
---

The AVC CLI is the primary interface — every other surface (VSCode extension, web UI, MCP server) calls these commands underneath. Each command supports `--json` for machine-readable output.

## Global flags

| Flag | Description |
|------|-------------|
| `--json` | Emit results as JSON to stdout instead of human-readable text |
| `--help` | Show help for any command |

Errors are written to `stderr`; the process exits with code `1` on failure.

## Commands by topic

### Project setup

| Command | Purpose |
|---------|---------|
| [`init`](/agentic-vc/cli/init/) | Initialize AVC for a project |

### Snapshots

| Command | Purpose |
|---------|---------|
| [`snapshot`](/agentic-vc/cli/snapshot/) | Create a snapshot |
| [`list`](/agentic-vc/cli/list/) | List snapshots on the active branch |
| [`info`](/agentic-vc/cli/info/) | Show snapshot metadata + file list |
| [`log`](/agentic-vc/cli/log/) | Show history as a tree |
| [`delete`](/agentic-vc/cli/delete/) | Delete a snapshot |

### Diff & restore

| Command | Purpose |
|---------|---------|
| [`diff`](/agentic-vc/cli/diff/) | Compare two snapshots |
| [`diff-current`](/agentic-vc/cli/diff-current/) | Compare a snapshot vs working tree |
| [`restore`](/agentic-vc/cli/restore/) | Roll back to a snapshot |
| [`restore-file`](/agentic-vc/cli/restore-file/) | Restore a single file |
| [`cat`](/agentic-vc/cli/cat/) | Print file contents from a snapshot |
| [`file-history`](/agentic-vc/cli/file-history/) | List snapshots containing a file |
| [`annotate`](/agentic-vc/cli/annotate/) | Show which snapshot introduced each line |

### Branches & merge

| Command | Purpose |
|---------|---------|
| [`branch`](/agentic-vc/cli/branch/) | Create / list / switch / delete / diff branches |
| [`merge`](/agentic-vc/cli/merge/) | Merge a branch into main with conflict detection |

### Agents & UI

| Command | Purpose |
|---------|---------|
| [`mcp`](/agentic-vc/cli/mcp/) | Start the MCP server for AI agents |
| [`ui`](/agentic-vc/cli/ui/) | Start the standalone web UI server |

## JSON output convention

Every command that returns structured data accepts `--json`. The shape mirrors what's documented per-command. Errors always include an `error` field:

```json
{ "error": "snapshot 'snap-foo' not found" }
```

For scripts and agents, prefer `--json` — it's stable across CLI text-output revisions.
