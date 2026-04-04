# AVC CLI Reference

All commands accept `--json` to emit machine-readable output on stdout.  
Errors are written to stderr; the process exits with code `1`.

---

## Global flags

| Flag | Description |
|------|-------------|
| `--json` | Output results as JSON instead of human-readable text |
| `--help` | Show help for any command |

---

## `avc init [project_path]`

Initialize AVC for a project. Creates `.avc/` with a SQLite database and default config.

```bash
avc init                    # initialize in current directory
avc init /path/to/project   # initialize at a specific path
avc init --json
```

**JSON output:**
```json
{
  "id": "proj-a1b2c3",
  "path": "/path/to/project",
  "name": "project",
  "success": true
}
```

Safe to run more than once — re-running on an already-initialized project is a no-op.

---

## `avc snapshot <label>`

Save the current state of the project as a named snapshot.

```bash
avc snapshot "Before refactor"
avc snapshot "v1.2.0 release" --agent "claude" --notes "Passed all tests"
avc snapshot "WIP" --json
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--agent <name>` | Name of the agent or user creating the snapshot |
| `--notes <text>` | Free-form notes attached to the snapshot |

**JSON output:**
```json
{
  "id": "snap-xyz789",
  "label": "v1.2.0 release",
  "timestamp": 1712289600,
  "agent_name": "claude",
  "files_changed": 42,
  "total_size": 1048576,
  "notes": "Passed all tests",
  "success": true
}
```

Files matching `.avcignore` patterns are excluded. The `.avc/` directory itself is always excluded.

---

## `avc list`

List all snapshots for the current project, newest first.

```bash
avc list
avc list --json
```

**JSON output:**
```json
[
  {
    "id": "snap-def456",
    "label": "Fixed bug in auth",
    "timestamp": 1712282400,
    "agent_name": "claude",
    "files_changed": 3,
    "total_size": 512000,
    "notes": "Security patch"
  },
  {
    "id": "snap-abc123",
    "label": "Before refactor",
    "timestamp": 1712275200,
    "agent_name": "",
    "files_changed": 12,
    "total_size": 524288,
    "notes": ""
  }
]
```

Returns an empty array `[]` if no snapshots exist.

---

## `avc diff <from_id> <to_id>`

Show file-by-file changes between two snapshots.

```bash
avc diff snap-abc123 snap-def456
avc diff snap-abc123 snap-def456 --json
```

**JSON output:**
```json
{
  "from_snapshot": "snap-abc123",
  "to_snapshot": "snap-def456",
  "files": [
    {
      "path": "src/auth.go",
      "type": "modified",
      "old_hash": "abc123...",
      "new_hash": "def456...",
      "lines_added": 5,
      "lines_removed": 2,
      "diff_preview": "+func NewAuth() Auth {\n+\treturn &authImpl{}\n"
    },
    {
      "path": "config/settings.go",
      "type": "added",
      "new_hash": "ghi789...",
      "lines_added": 20,
      "lines_removed": 0
    },
    {
      "path": "old/legacy.go",
      "type": "deleted",
      "old_hash": "jkl012...",
      "lines_added": 0,
      "lines_removed": 45
    }
  ]
}
```

**Change types:** `added` · `modified` · `deleted`

---

## `avc restore <snapshot_id>`

Roll the project back to the state captured in a snapshot.

```bash
avc restore snap-abc123
avc restore snap-abc123 --json
```

**JSON output:**
```json
{
  "id": "snap-abc123",
  "restored_files": 12,
  "restored_size": 524288,
  "success": true,
  "message": "Successfully restored snapshot snap-abc123"
}
```

> **Warning:** This overwrites current files. Take a snapshot first if you want to preserve the current state before restoring.

Files that were not tracked in the snapshot are left untouched.

---

## `avc info <snapshot_id>`

Show detailed metadata and the full file list for a snapshot.

```bash
avc info snap-abc123
avc info snap-abc123 --json
```

**JSON output:**
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

---

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error (snapshot not found, project not initialized, I/O failure, etc.) |

---

## `.avcignore`

Place a `.avcignore` file in the project root to exclude files and directories from all snapshots. Syntax is identical to `.gitignore`.

**Default patterns (written by `avc init`):**
```
node_modules/
vendor/
dist/
build/
.env
.DS_Store
```

The `.avc/` directory is always excluded regardless of `.avcignore` contents.
