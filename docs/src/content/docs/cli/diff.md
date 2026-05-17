---
title: avc diff
description: Compare two snapshots file-by-file.
---

Shows added, modified, and deleted files between two snapshots, with line-level counts and unified diff previews.

## Usage

```bash
avc diff snap-abc123 snap-def456
avc diff snap-abc123 snap-def456 --json
```

## Arguments

| Argument | Required | Description |
|----------|---------|-------------|
| `<from_id>` | yes | Older snapshot ID |
| `<to_id>` | yes | Newer snapshot ID |

## JSON output

```json
{
  "from_snapshot": "snap-abc123",
  "to_snapshot": "snap-def456",
  "files": [
    {
      "path": "src/auth.go",
      "type": "modified",
      "old_hash": "abc...",
      "new_hash": "def...",
      "lines_added": 5,
      "lines_removed": 2,
      "diff_preview": "+func NewAuth() Auth {\n+  return &authImpl{}\n"
    },
    {
      "path": "src/old.go",
      "type": "deleted",
      "old_hash": "ghi...",
      "lines_added": 0,
      "lines_removed": 45
    }
  ]
}
```

**Change types:** `added` · `modified` · `deleted`

## How it works

AVC reads both snapshots' file lists and builds a per-path comparison:

| Old | New | Type |
|:---:|:---:|------|
| ✓ | ✓ (same hash) | (skipped — no change) |
| ✓ | ✓ (different hash) | `modified` |
| ✗ | ✓ | `added` |
| ✓ | ✗ | `deleted` |

For each changed file, the unified diff is generated from the stored object blobs using an LCS (longest common subsequence) algorithm.

## See also

- [`avc diff-current`](/agentic-vc/cli/diff-current/) — compare a snapshot to the working tree instead of another snapshot
