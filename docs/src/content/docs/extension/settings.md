---
title: Configuration
description: Settings to customize the extension behavior.
sidebar:
  order: 6
---

All settings live under `avc.*`. Edit them in the VSCode settings UI (`Ctrl+,`) or directly in your `settings.json`.

## Core settings

| Setting | Default | Description |
|---------|---------|-------------|
| `avc.cliPath` | `"avc"` | Path to the `avc` CLI binary. Override if it's not on `PATH` — e.g. `"C:/Users/you/go/bin/avc.exe"` |
| `avc.projectPath` | `""` | Override the project root. Defaults to the first workspace folder. |
| `avc.defaultAgentName` | `""` | Auto-fills the agent name when creating a snapshot. Useful if you always want the same identifier (e.g. your name or `"manual"`). |

## Auto-snapshot settings

The extension can automatically create snapshots after you save files. Disabled by default.

| Setting | Default | Description |
|---------|---------|-------------|
| `avc.autoSnapshot.enabled` | `false` | Master switch. When `true`, the extension watches file saves and creates snapshots in the background. |
| `avc.autoSnapshot.debounceSeconds` | `30` | Wait this long after the last save before snapshotting. Higher values group more changes into one snapshot. |
| `avc.autoSnapshot.cooldownMinutes` | `5` | Minimum gap between auto-snapshots. Prevents the snapshot list from growing too quickly during heavy editing. |

## Example settings.json

```json
{
  "avc.cliPath": "avc",
  "avc.defaultAgentName": "manual",
  "avc.autoSnapshot.enabled": true,
  "avc.autoSnapshot.debounceSeconds": 60,
  "avc.autoSnapshot.cooldownMinutes": 10
}
```

:::caution
Auto-snapshots are labeled `"Auto-snapshot"` with the configured agent name. They participate in the **Type** filter (sidebar & timeline) so you can hide or show them on demand.
:::

## Toggle line annotations

Run `Ctrl+Shift+P` → **AVC: Toggle Line Annotations** to show or hide inline gutter annotations indicating which snapshot introduced each line. There's no setting — it's a per-session toggle.
