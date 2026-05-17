---
title: avc diff-current
description: Compare a snapshot against the current working tree.
---

Shows what's different between a snapshot and the files currently on disk. Same JSON shape as [`avc diff`](/agentic-vc/cli/diff/).

## Usage

```bash
avc diff-current snap-abc123
avc diff-current snap-abc123 --json
```

## When to use it

- **"What did I change since my last snapshot?"** — compare against your most recent snapshot
- **"What would change if I restored?"** — see the impact before running `avc restore`
- **VSCode Source Control** — the extension calls this to populate the SCM panel

## JSON output

Identical to `avc diff` but with `to_snapshot` set to `"working-tree"`:

```json
{
  "from_snapshot": "snap-abc123",
  "to_snapshot": "working-tree",
  "files": [
    {
      "path": "src/auth.go",
      "type": "modified",
      "old_hash": "abc...",
      "new_hash": null,
      "lines_added": 3,
      "lines_removed": 1,
      "diff_preview": "+ new line"
    }
  ]
}
```

The `new_hash` is `null` because the working-tree file isn't (necessarily) in the object store yet.
