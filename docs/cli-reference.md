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
  "quarantined_files": 1,
  "trash_op_id": "2026-07-09T15-04-05-restore-af2202",
  "undo_snapshot_id": "snap-def456",
  "success": true,
  "message": "Successfully restored snapshot snap-abc123"
}
```

> **Warning:** This overwrites current files. AVC protects you automatically — see below — but a snapshot of your own before restoring is always a good habit.

Files matched by `.avcignore` (e.g. `.env`, `node_modules/`) are never touched by restore — they are not part of any snapshot, so deleting them would be permanent data loss. Any other file present on disk but absent from the target snapshot is moved to `.avc/trash/` instead of being deleted, so a restore can never destroy data irrecoverably. See [`avc trash`](#avc-trash) to inspect or reclaim quarantined files.

**Automatic safety snapshot:** if the working tree has changed since its last snapshot, AVC captures that state first, before overwriting anything. `undo_snapshot_id` is empty when the tree was already clean (nothing needed capturing); otherwise it names the snapshot to restore if you want to undo this restore itself:

```bash
avc restore snap-def456   # undoes the restore above
```

---

## `avc merge <branch>`

Perform a three-way merge of an agent branch into main. Always auto-snapshots
main first, so a merge is never a one-way door — see `--abort` below.

```bash
avc merge feat/my-branch             # merge
avc merge feat/my-branch --preview   # dry run — no writes, no recorded merge
avc merge --abort                    # roll back the last in-progress/conflicted/failed merge
```

**Decisions per file:**

| Decision | Meaning |
|----------|---------|
| `clean`    | Only the branch changed (or added a new file) — applied automatically |
| `delete`   | The branch deleted a file main left unchanged — removed from main |
| `conflict` | Both main and branch changed the same file — written with conflict markers |
| `skip`     | No net change relative to the merge base — left untouched |

**JSON output:**
```json
{
  "merge_id": "merge-abc123",
  "branch": "feat/my-branch",
  "preview": false,
  "clean": 3,
  "deleted": 1,
  "conflicts": 0,
  "skipped": 12,
  "files": [{ "path": "src/auth.go", "decision": "clean" }],
  "post_merge_snapshot_id": "snap-def456",
  "auto_snapshot_id": "snap-ghi789"
}
```

- `auto_snapshot_id` appears when the branch's workspace had changes since its last snapshot — those changes are captured automatically before the merge runs, so un-snapshotted work is never silently dropped. `--preview` never creates this snapshot (it has no side effects); instead its output includes `workspace_dirty_files` as a warning that the preview does not yet reflect those changes.
- A conflict between an edit and a deletion is written with a labeled diff3 marker (e.g. `>>>>>>> branch (theirs) — file deleted on branch`) so it's clear which side removed the file.
- Merge status values: `in_progress` → `completed` | `conflicts` | `failed` | `aborted`. A merge that fails partway through applying its plan is marked `failed` (not left stuck at `in_progress`), so `avc merge --abort` can always find and roll back the last attempt regardless of how it ended.

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

## `avc trash`

Inspect or reclaim files quarantined by `avc restore`. AVC never permanently
deletes an untracked file during a restore — it moves it to `.avc/trash/<opID>/`
instead, so nothing a restore removes is unrecoverable until you explicitly
empty the trash.

### `avc trash list`

```bash
avc trash list
avc trash list --json
```

**JSON output:**
```json
[
  {
    "op_id": "2026-07-09T15-04-05-restore-af2202",
    "kind": "restore",
    "created_at": "2026-07-09T15:04:05-04:00",
    "files": ["untracked.txt"]
  }
]
```

### `avc trash empty`

Permanently deletes quarantined entries. Defaults to removing everything;
pass `--older-than` to only remove entries older than a given duration.

```bash
avc trash empty                    # remove all entries
avc trash empty --older-than 24h   # remove entries older than 24 hours
avc trash empty --json
```

Every restore also opportunistically clears trash entries older than 7 days
on its own (best-effort — never blocks the restore it runs alongside).

---

## `avc delete <snapshot_id>`

Delete a snapshot and its stored file objects.

```bash
avc delete snap-abc123
avc delete snap-abc123 --force   # required for a protected snapshot
```

A snapshot is **protected** — refused without `--force` — if it is:
- the base snapshot of an active branch,
- carrying a tag (`avc snapshot tag`), or
- part of the most recent merge record for its branch.

This prevents pruning or a stray `avc delete` from corrupting an active
branch's merge base or silently untagging a milestone. The same protection
applies to automatic retention pruning (`[retention]` in config.toml) and to
the MCP `avc_delete` tool, which has no `--force` equivalent — an agent can
never delete a protected snapshot on its own judgment; ask the user to run
`avc delete --force` if that's genuinely intended.

---

## `avc gc`

Scan `.avc/objects/` and remove blobs no longer referenced by any snapshot.

```bash
avc gc                  # dry run (default) — reports what would be removed
avc gc --run             # actually delete and reclaim disk space
avc gc --grace 0         # disable the grace period (see below)
```

**JSON output:**
```json
{
  "scanned_objects": 42,
  "deleted_objects": 3,
  "bytes_reclaimed": 15360,
  "skipped_recent": 1,
  "dry_run": true
}
```

Objects (and stray temp files from an interrupted write) younger than
`--grace` (default `15m`) are always kept, even with `--run`. A snapshot
writes its objects to disk *before* its database row exists, so an object
that looks unreferenced for that brief window might belong to a snapshot
still being created concurrently — the grace period prevents GC from racing
that write and deleting a blob the snapshot is about to reference.

---

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error (snapshot not found, project not initialized, I/O failure, etc.) |

---

## `.avcignore`

Place a `.avcignore` file in the project root to exclude files and directories from all snapshots. Syntax follows `.gitignore`:

- A pattern with no `/` (other than an optional trailing one) matches at **any depth** — `node_modules/` excludes `node_modules` wherever it appears, not just at the project root.
- `**` matches zero or more path segments — `**/*.log` matches `app.log` and `logs/deep/nested/app.log` alike; `src/**/generated` matches any `generated` directory under `src/`.
- A trailing `/` restricts the pattern to directories only.
- A leading `!` un-ignores a path an earlier, broader pattern excluded — patterns are applied in file order and the **last** match wins, so put exceptions after the rule they narrow:
  ```
  *.log
  !keep.log
  ```
- On a branch, if the workspace has its own `.avcignore` (e.g. an agent added one as part of its task), snapshots taken from that workspace honor the workspace's copy, not the project root's.

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

### Large files

Files larger than `[snapshot] max_file_size_mb` in `.avc/config.toml` (default
100 MB) are skipped — not read, hashed, or stored — with a warning printed to
stderr and listed in the snapshot's `skipped_large` field. This protects
against an out-of-memory read on an accidentally-tracked large binary.
