---
title: avc info
description: Show snapshot metadata and full file list.
---

Detailed view of a single snapshot, including every file with its hash and size.

## Usage

```bash
avc info snap-abc123
avc info snap-abc123 --json
```

## JSON output

```json
{
  "id": "snap-abc123",
  "label": "Before refactor",
  "timestamp": 1712275200,
  "agent_name": "claude",
  "notes": "Stable baseline",
  "file_count": 12,
  "total_size": 524288,
  "files": [
    { "path": "main.go",      "hash": "abc...", "size": 1024 },
    { "path": "src/auth.go",  "hash": "def...", "size": 4096 }
  ]
}
```

## See also

- [`avc cat`](/agentic-vc/cli/cat/) — print a file's contents from this snapshot
- [`avc restore-file`](/agentic-vc/cli/restore-file/) — restore one file from this snapshot
