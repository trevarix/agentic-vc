---
title: avc restore-file
description: Restore a single file from a snapshot.
---

Restores one file rather than the whole snapshot.

## Usage

```bash
avc restore-file snap-abc123 src/auth.go
avc restore-file snap-abc123 src/auth.go --json
```

## Arguments

| Argument | Required | Description |
|----------|---------|-------------|
| `<snapshot_id>` | yes | Snapshot to read from |
| `<file_path>` | yes | Project-relative file path |

## JSON output

```json
{
  "id": "snap-abc123",
  "file_path": "src/auth.go",
  "size": 4096,
  "success": true
}
```

## Notes

- The file is **overwritten** if it exists; **created** if it doesn't
- Other files in the project are not touched
- If the file isn't in the snapshot, you get a 404-style error and no file is written
- Useful for restoring a single config or one buggy file without rolling back everything else
