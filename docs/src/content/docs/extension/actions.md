---
title: Snapshot Actions
description: Five buttons appear under each expanded snapshot.
sidebar:
  order: 3
---

Click the chevron next to any snapshot to expand it. Five action rows appear directly below — no hover required, no right-clicking.

## Available actions

| Action | What it does |
|--------|--------------|
| **View Details** | Opens the snapshot detail webview with the full file tree |
| **View Diff (vs previous)** | Compare against the next-older snapshot in the list |
| **Diff with Current Files** | Compare snapshot against your current working tree |
| **Restore This Snapshot** | Roll the entire project back to this snapshot |
| **Delete Snapshot** | Permanently delete this snapshot |

## Confirmation modals

Destructive actions (**Restore**, **Delete**) always show a confirmation dialog. Cancelling closes the dialog with no side effects.

:::tip[Safety net]
Before any restore, the extension auto-creates a snapshot labeled `"Pre-restore safety snapshot"`. If the restore was a mistake, just restore that pre-restore snapshot to undo it.
:::

## Right-click menu

The same five actions are also available in the right-click context menu on any snapshot row, plus they appear in the Command Palette under the **AVC:** prefix.
