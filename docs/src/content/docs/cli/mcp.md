---
title: avc mcp
description: Start the MCP (Model Context Protocol) server for AI agents.
---

Starts an MCP JSON-RPC 2.0 server over stdio, exposing AVC commands as tools that AI agents can call directly.

## Usage

```bash
avc mcp serve              # default pretty-printed output
avc mcp serve --compact    # compact JSON for token-sensitive contexts
```

## Flags

| Flag | Description |
|------|-------------|
| `--compact` | Emit single-line JSON without indentation (saves tokens for LLM context windows) |

## Available tools

The server exposes 14 tools that map directly to the CLI:

| Tool | CLI equivalent |
|------|----------------|
| `avc_snapshot` | `avc snapshot` |
| `avc_list` | `avc list` |
| `avc_diff` | `avc diff` |
| `avc_restore` | `avc restore` |
| `avc_info` | `avc info` |
| `avc_delete` | `avc delete` |
| `avc_branch_create` | `avc branch create` |
| `avc_branch_list` | `avc branch list` |
| `avc_branch_switch` | `avc branch switch` |
| `avc_branch_diff` | `avc branch diff` |
| `avc_merge_preview` | `avc merge --preview` |
| `avc_merge` | `avc merge` |
| `avc_merge_abort` | `avc merge --abort` |
| `avc_run_in_workspace` | run a shell command inside a branch workspace |

## Setup for agent frameworks

Don't configure the MCP server by hand — let AVC do it:

```bash
avc init --skills claude-code,cursor,windsurf
```

This writes the right MCP config files for each framework. See [Agent Integration](/agentic-vc/agents/) for details.

## Manual integration

If you're building a custom agent or testing the server manually:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | avc mcp serve
```

The server reads JSON-RPC 2.0 messages on stdin and writes responses on stdout. It exits when stdin closes.
