---
name: avc-setup
description: Verify AVC is installed and the project is initialized — run this when avc_* tools are missing or return "not an AVC project"
---

Use this skill when the `avc_*` tools are unavailable, or when a tool call reports that the current directory is not an AVC project.

AVC is a local binary. The plugin registers the MCP server, but it cannot install the binary or initialize a project for the user — both steps happen on their machine.

## Step 1 — check the binary

Run `avc --version`.

If the command is not found, AVC is not installed or not on `PATH`. Give the user the install command for their platform and stop until they confirm:

| Platform | Command |
|----------|---------|
| macOS | `brew install trevarix/tap/avc` |
| Windows | `scoop bucket add trevarix https://github.com/trevarix/scoop-bucket` then `scoop install avc` |
| Linux | Download the release archive from https://github.com/trevarix/agentic-vc/releases and move `avc` to `/usr/local/bin/` |
| Any (Go 1.22+) | `go install github.com/trevarix/agentic-vc/avc@latest` |

After they install, tell them to restart Claude Code so the MCP server picks up the new binary.

## Step 2 — check the project

Run `avc status --json`.

If it reports that this is not an AVC project, ask the user whether to initialize it, then run:

```
avc init
```

This creates `.avc/` with a SQLite database, a default `.avcignore`, and `config.toml`. It does not modify their source files.

Never run `avc init` without asking — it writes to the project root and adds entries to `.gitignore`.

## Step 3 — confirm the tools

Once the binary and project both check out, confirm the `avc_*` tools respond by calling **avc_status**.

If the tools are still missing after a restart, the MCP server is not starting. Ask the user to run `avc mcp serve` in a terminal and share any error it prints.

## Claude Desktop

Plugin-bundled MCP servers do not run in Claude Desktop chat — only skills do. A Desktop user who wants the `avc_*` tools needs the AVC desktop extension instead: **Settings → Extensions → Advanced settings → Install Extension…** and select the `avc.mcpb` file from the [latest release](https://github.com/trevarix/agentic-vc/releases).
