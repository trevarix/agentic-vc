---
title: Overview
description: The standalone web UI served by `avc ui`.
sidebar:
  order: 1
---

`avc ui` serves a complete graphical interface for managing snapshots — no VSCode required. It's the best option for users on Cursor, JetBrains, Neovim, Sublime, plain terminal, or anyone who wants a non-editor browser.

## Launch

```bash
avc ui
```

Output:

```
AVC UI server listening at http://127.0.0.1:3004
Opening http://127.0.0.1:3004 in your default browser…
```

The browser opens automatically. Stop the server with `Ctrl+C`.

## What you get

| Feature | Description |
|---------|-------------|
| **Snapshot list** | Grouped by date (Today / Yesterday / etc.), filterable by agent / type / branch |
| **Detail panel** | Metadata + collapsible folder tree of every file in the snapshot |
| **Diff viewer** | Side-by-side unified diff with syntax highlighting |
| **Restore** | One-click restore with confirmation (and auto safety snapshot) |
| **File-level restore** | Restore individual files from the detail panel |
| **Save snapshot** | Modal with label / agent / notes fields |
| **Timeline** | Vertical visual history of snapshots |
| **Documentation** | Built-in `/docs.html` page with full command reference |

## Design

The UI is plain HTML/CSS/JS — no React, no Vue, no build step. Assets are embedded into the Go binary via `//go:embed`, so the entire web app ships inside the single `avc` binary. There's nothing to install separately.

This keeps the install footprint to one ~15 MB executable that gives you a CLI **and** a web app.

## Theme

The UI auto-detects your OS theme (light / dark via `prefers-color-scheme`) and uses a GitHub-inspired palette. There's a manual toggle in the top-right of every page.

## Multi-user safety

By default the server binds to `127.0.0.1` only — nobody outside your machine can reach it. There's no authentication, by design (it's meant for local use only).

If you bind with `--host 0.0.0.0`, **anyone on your LAN** can read, restore, and delete your snapshots. Use that only on networks you trust.

## See also

- [`avc ui` command reference](/agentic-vc/cli/ui/)
- [REST API](/agentic-vc/web-ui/api/) — the HTTP endpoints behind the UI
