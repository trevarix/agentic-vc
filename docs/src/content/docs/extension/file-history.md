---
title: File History
description: Right-click any file to see every snapshot it appears in.
sidebar:
  order: 5
---

Like `git blame` but for AVC snapshots — instantly see every snapshot that contains a file, with quick actions to view, diff, or restore old versions.

## Where to invoke it

- Right-click a file in the **Explorer** → **AVC: Show File History**
- Right-click anywhere in an open **editor** → **AVC: Show File History**
- Right-click an editor **tab title** → **AVC: Show File History**
- Or run `Ctrl+Shift+P` → **AVC: Show File History** (uses the active editor)

## The flow

1. A QuickPick appears listing every snapshot that contains the file, newest first. Each row shows the snapshot label, timestamp, agent, file size, and short hash.
2. Pick a version — a second QuickPick offers three actions:
   - **Open in editor** — opens the file as it was in that snapshot, read-only
   - **Diff against current** — opens VSCode's native side-by-side diff editor (with code folding, theme, gutter indicators)
   - **Restore this version** — overwrites the working file with the snapshot version (confirmation prompt first)
3. The chosen action runs immediately. For **Restore**, you'll see a confirmation toast.

## Native diff editor

The "Diff against current" action uses a custom URI scheme (`avc-snapshot://`) so VSCode's **native** diff editor handles the rendering — same UI as Git diffs, with all the editor features (search, fold, gutter blame, etc.).

:::tip
If the file isn't found in any snapshot, you'll see *"File not found in any snapshot"* instead of an empty QuickPick.
:::
