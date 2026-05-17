---
title: avc file-history
description: List every snapshot that contains a specific file.
---

Shows every snapshot containing the given file, with its hash and size in each one.

## Usage

```bash
avc file-history src/auth.go
avc file-history src/auth.go --json
```

## JSON output

```json
[
  {
    "snapshot_id": "snap-def456",
    "label": "Fixed auth bug",
    "timestamp": 1712282400,
    "agent_name": "claude",
    "hash": "def456...",
    "size": 4096
  },
  {
    "snapshot_id": "snap-abc123",
    "label": "Initial",
    "timestamp": 1712275200,
    "agent_name": "",
    "hash": "abc123...",
    "size": 3984
  }
]
```

Order is newest first by snapshot timestamp.

## When to use it

- **"When did this file last change?"** — look at the most recent two entries' hashes
- **"Did the agent ever touch this file?"** — filter the result by `agent_name`
- **VSCode extension** — right-click a file → "AVC: Show File History" calls this and offers quick actions
