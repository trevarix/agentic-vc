---
title: Source Control Panel
description: AVC integrates with the standard Source Control view.
sidebar:
  order: 4
---

Open the Source Control panel with `Ctrl+Shift+G`. AVC appears as its own section alongside Git, listing every file that's changed since the latest snapshot.

## What you see

- **Changes since last snapshot** — one row per modified, added, or deleted file
- **Color-coded icons** — green for added, yellow for modified, red strikethrough for deleted
- **Tooltip** on each file shows the line counts (`+5 -2`)
- **Snapshot input box** at the top with placeholder *"Snapshot label..."*

## Quick snapshot from the SCM panel

1. Type a label into the input box (e.g. `"Fix login bug"`)
2. Press `Ctrl+Enter` (or click the checkmark)
3. The snapshot is created with your label — equivalent to running `avc snapshot "Fix login bug"`

## Refresh behavior

The panel updates automatically:

- On extension activation
- 2 seconds after any file save (debounced)
- After any snapshot, restore, or delete operation

:::tip
If a Git-tracked project also has AVC enabled, you'll see **both** sections in the Source Control panel — Git for repository-level changes, AVC for snapshot-level changes since your last save.
:::
