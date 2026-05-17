---
title: Sidebar Overview
description: Navigate snapshots, branches, and changes from the activity bar.
sidebar:
  order: 2
---

Click the camera icon in the activity bar to open the AVC panel. Everything you need is one or two clicks away.

## Anatomy

```
┌──────────────────────────────────────────────┐
│ AVC SNAPSHOTS                  [+ ↻ ⌕ ⇄ ⏱]  │  ← header buttons
├──────────────────────────────────────────────┤
│ ▼ Today (3)                                  │
│   ▶ Auto-snapshot     4/19/2026 12:09 AM    │  ← collapsed snapshot
│   ▶ Manual baseline   4/19/2026 12:08 AM    │
│ ▶ Yesterday (1)                              │
│ ▶ This Week (5)                              │
│ ▶ Older (12)                                 │
└──────────────────────────────────────────────┘
AVC: 21 snapshots  ◊ main  +0 ~2 -0
```

## Header buttons

| Button | Command | What it does |
|--------|---------|--------------|
| **+** | `avc.saveSnapshot` | Prompts for label and notes, creates a snapshot |
| **↻** | `avc.refreshSnapshots` | Reload the snapshot list and SCM stats |
| **⌕** | `avc.filterSnapshots` | Multi-step filter by Agent / Type / Branch |
| **⇄** | `avc.compareTwoSnapshots` | Pick any two snapshots to diff |
| **⏱** | `avc.showTimeline` | Open the visual timeline webview |
| **⊕** | `avc.createBranch` | Create a new branch from current snapshot |
| **⨉** | `avc.mergeBranch` | Merge a branch into main with preview |

## Date grouping

Snapshots automatically bucket into **Today / Yesterday / This Week / This Month / Older**. Groups start collapsed; click any header to expand. The number next to each group is its snapshot count.

## Status bar (bottom-left)

| Item | Click action |
|------|--------------|
| `AVC: N snapshots` | Refresh the list |
| `◊ <branch>` | QuickPick to switch branches |
| `+A ~M -D` | Open diff vs the latest snapshot (only shown when there are changes) |
