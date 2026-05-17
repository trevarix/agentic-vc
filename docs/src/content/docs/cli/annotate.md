---
title: avc annotate
description: Show which snapshot introduced each line of a file.
---

Like `git blame`, but for AVC snapshots. Traces every line in a file back to the snapshot that introduced it.

## Usage

```bash
avc annotate src/auth.go
avc annotate src/auth.go --json
```

## Output

Human output shows one line per file line, each prefixed with the snapshot ID and agent that introduced it:

```
snap-abc123 claude   1: package auth
snap-abc123 claude   2:
snap-def456 alice    3: import (
snap-def456 alice    4:     "crypto/bcrypt"
snap-def456 alice    5: )
```

## JSON output

```json
{
  "file_path": "src/auth.go",
  "lines": [
    { "line": 1, "snapshot_id": "snap-abc123", "agent_name": "claude" },
    { "line": 2, "snapshot_id": "snap-abc123", "agent_name": "claude" },
    { "line": 3, "snapshot_id": "snap-def456", "agent_name": "alice" }
  ]
}
```

## When to use it

- Investigating *who* introduced a specific line (you or which agent)
- Tracing a bug back to the snapshot that introduced it
- VSCode extension uses this for inline gutter annotations
