---
title: avc ui
description: Start the standalone web UI server.
---

Serves a graphical interface at `http://127.0.0.1:3004` for users who don't run VSCode. Auto-opens your browser. Same features as the VSCode extension.

## Usage

```bash
avc ui                             # default port 3004, auto-open browser
avc ui --port 8080                 # custom port
avc ui --no-open                   # don't open browser (headless / SSH)
avc ui --host 0.0.0.0              # bind all interfaces (LAN access)
```

## Flags

| Flag | Description |
|------|-------------|
| `--port <n>` | Port to listen on (default `3004`) |
| `--host <addr>` | Bind host (default `127.0.0.1` — localhost only) |
| `--no-open` | Don't open the browser automatically |

## Lifecycle

The server runs in the foreground. Stop it with `Ctrl+C`.

```
$ avc ui
AVC UI server listening at http://127.0.0.1:3004
Opening http://127.0.0.1:3004 in your default browser…
```

## Network exposure

:::caution
Binding to `0.0.0.0` exposes the UI to your local network **with no authentication**. Anyone on the LAN can browse snapshots, restore, and delete. Only use on trusted networks.
:::

The default `127.0.0.1` binding is loopback-only and safe by default.

## See also

- [Web UI documentation](/agentic-vc/web-ui/) — features and screenshots
- [REST API reference](/agentic-vc/web-ui/api/) — the HTTP endpoints behind the UI
