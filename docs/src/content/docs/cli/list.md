---
title: avc list
description: List all snapshots on the active branch, newest first.
---

Lists all snapshots on the current branch, newest first.

## Usage

```bash
avc list
avc list --json
```

## JSON output

```json
[
  {
    "id": "snap-def456",
    "label": "Fixed bug in auth",
    "timestamp": 1712282400,
    "agent_name": "claude",
    "files_changed": 3,
    "total_size": 512000,
    "notes": "Security patch",
    "branch_id": "br-main"
  }
]
```

Returns an empty array `[]` if no snapshots exist.

## Notes

- Only snapshots on the **active branch** are returned. Use `avc branch switch <name>` to see another branch's snapshots
- Order is newest first by `timestamp`
- For tree-style history output, use [`avc log`](/agentic-vc/cli/log/) instead
