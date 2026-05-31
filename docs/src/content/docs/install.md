---
title: Installation
description: Install the AVC CLI and optional clients (VSCode extension, web UI).
---

AVC ships as a single Go binary. Install once, use anywhere — terminal, VSCode, browser, or via MCP from an AI agent.

## CLI

### macOS — Homebrew

```bash
brew install skillmythorg/tap/avc
```

### Linux / macOS — one-line install

```bash
curl -sSL https://raw.githubusercontent.com/trevarix/agentic-vc/main/install.sh | sh
```

### Windows — PowerShell

```powershell
irm https://raw.githubusercontent.com/trevarix/agentic-vc/main/install.ps1 | iex
```

### Build from source

Requires Go 1.22 or newer.

```bash
git clone https://github.com/trevarix/agentic-vc.git
cd agentic-vc/avc
go install .
```

This drops `avc` into `~/go/bin/` (or `%USERPROFILE%\go\bin\` on Windows). Make sure that directory is on your `PATH`.

### Verify

```bash
avc --version
avc --help
```

You should see the version number and a list of commands. If not, double-check that the install location is on your `PATH`.

## VSCode Extension

The extension provides a sidebar, Source Control panel integration, and file history for snapshots — without leaving VSCode.

:::note
The extension calls the `avc` CLI under the hood, so install the CLI first.
:::

### From the Marketplace

1. Open VSCode → Extensions (`Ctrl+Shift+X` / `Cmd+Shift+X`)
2. Search for **AVC — Agentic Version Control**
3. Click **Install**

### From a VSIX

1. Download the latest `.vsix` from [GitHub Releases](https://github.com/trevarix/agentic-vc/releases)
2. In VSCode: `Ctrl+Shift+P` → **Extensions: Install from VSIX…**
3. Pick the downloaded file

See the [VSCode Extension guide](/agentic-vc/extension/) for a tour of the features.

## Web UI

Already bundled in the CLI — no separate install. Just run:

```bash
avc ui
```

Your default browser opens to `http://127.0.0.1:3004/`. Use this if you don't use VSCode or want to share a snapshot browser with non-technical teammates.

See the [Web UI guide](/agentic-vc/web-ui/) for screenshots and the REST API reference.

## MCP server (for AI agents)

Agents that speak the [Model Context Protocol](https://modelcontextprotocol.io/) can call AVC as a tool. The server is built into the CLI:

```bash
avc mcp serve
```

To configure popular agent frameworks automatically:

```bash
avc init --skills claude-code,cursor,windsurf
```

This writes the right config files for each framework into your project. See [Agent Integration](/agentic-vc/agents/) for details.

## Requirements

| Component | Requirement |
|-----------|-------------|
| OS | macOS 10.15+, Linux (any modern distro), Windows 10+ |
| Architecture | x86_64 or arm64 |
| Disk | ~15 MB for the binary |
| Go (only if building from source) | 1.22+ |

AVC has no runtime dependencies — no Python, no Node.js, no CGO. The SQLite implementation is pure Go.

## Next steps

After installing, walk through the [Quick Start guide](/agentic-vc/quick-start/) to take your first snapshot.
