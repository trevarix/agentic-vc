---
title: avc cat
description: Print file contents from a snapshot to stdout.
---

Outputs the raw bytes of a file as it was stored in a snapshot. Useful for piping into other tools or capturing an old version into a different filename.

## Usage

```bash
avc cat snap-abc123 src/auth.go                # write raw bytes to stdout
avc cat snap-abc123 src/auth.go > old-auth.go  # save as a different file
avc cat snap-abc123 src/auth.go --json         # base64-encoded for binary safety
```

## JSON output

```json
{
  "snapshot_id": "snap-abc123",
  "file_path": "src/auth.go",
  "content_base64": "cGFja2FnZSBhdXRoCgo..."
}
```

The base64 encoding makes binary files safe to ship over JSON. Decode with `base64 -d` or your language's standard library.

## Behaviour

- Binary-safe — raw mode writes bytes verbatim; JSON mode base64-encodes them
- No file is created on disk; output is purely stdout
- Errors (snapshot missing, file missing in snapshot) go to stderr with exit code 1
