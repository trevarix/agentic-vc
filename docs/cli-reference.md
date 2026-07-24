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
| `--session <id>` | Agent session this snapshot belongs to — `avc timeline` groups by it |
| `--task <text>` | One-line description of the session's overall task |

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
  "session_id": "sess-42",
  "task": "add auth endpoints",
  "summary": "2 files: modified auth.go (+40 -12), added auth_test.go",
  "success": true
}
```

`summary` is a heuristic one-liner describing what changed versus the
previous snapshot on the branch (empty for a branch's first snapshot). The
per-file fragments are persisted in the diff cache and reused by
`avc timeline`.

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

Returns an empty array `[]` if no snapshots exist. Each entry carries
`session_id` and `task` when the snapshot was created with session
attribution.

---

## `avc timeline`

Render a branch's history as a story: snapshots grouped by the agent session
that produced them, each with its one-line change summary, interleaved with
the restores, merges, and undos from the operations log. This is the "what
did my agents do while I was away" report.

```bash
avc timeline                     # active branch, all sessions
avc timeline --session sess-42   # one session's story
avc timeline --branch main       # a specific branch
avc timeline --limit 100 --json
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--session <id>` | Show only this session |
| `--branch <name>` | Branch to show (default: active branch) |
| `--limit <n>` | Max snapshots to include (default 50) |

**JSON output:**
```json
{
  "branch": "main",
  "sessions": [
    {
      "session_id": "sess-42",
      "task": "add auth endpoints",
      "agents": ["claude"],
      "started_at": 1712275200,
      "ended_at": 1712278800,
      "events": [
        {"kind": "snapshot", "timestamp": 1712275200, "snapshot_id": "snap-abc",
         "label": "auto: before auth refactor", "agent_name": "claude",
         "file_count": 42, "summary": "1 file: modified auth.go (+40 -12)"},
        {"kind": "operation", "timestamp": 1712278800, "op_kind": "restore",
         "details": "restored snapshot snap-abc"}
      ]
    }
  ]
}
```

Sessions come from the `session_id`/`task` attribution on snapshots
(`avc snapshot --session --task`, or the matching MCP `avc_snapshot`
arguments). Unattributed snapshots appear under `(no session)`. Summaries
missing from older snapshots are computed (and cached) lazily. Also served
by the web UI at `/api/timeline`.

---

## `avc watch`

Continuously checkpoint the project as files change. A foreground daemon
watches the project root — and every active branch workspace — and snapshots
after each burst of changes, so every state the project passes through is
recoverable whether or not an agent remembered to call `avc_snapshot`.

```bash
avc watch                 # start watching (foreground; Ctrl+C to stop)
avc watch --status        # is a watcher running for this project?
avc watch --poll 15       # poll every 15s instead of file events
                          # (for network filesystems with unreliable events)
```

Behavior:

- **Debounced.** A checkpoint is taken only after a quiet period
  (`debounce_seconds`, default 30) and no more often than
  `min_interval_seconds` (default 120) per branch.
- **Deduplicated.** A tree identical to the branch HEAD produces no
  snapshot — an idle project generates zero checkpoints. The stat cache
  makes the check nearly free.
- **Scoped.** Edits in a branch workspace checkpoint to that branch;
  ignored-file churn (build output, logs) triggers nothing.
- **Labeled.** Checkpoints are labeled `auto:watch <what changed>` with
  agent `avc-watch`, and are the first candidates for retention pruning
  (see below).
- **Single-instance.** A pid file (`.avc/watch.pid`) with a heartbeat
  refuses a second watcher; a stale file from a crashed daemon is replaced
  after 90s.

Configuration:

```toml
[watch]
debounce_seconds     = 30
min_interval_seconds = 120
include_workspaces   = true

[retention]
# Watch checkpoints are pruned before any other rule considers them.
# 0 = the built-in default (200); -1 = unlimited.
max_watch_snapshots_per_branch = 200
```

The VSCode extension can manage the daemon for you: enable
`avc.watch.enabled` and the watcher starts and stops with the editor.

---

## `avc bisect`

Find the first snapshot that broke a test command — binary search over
snapshot history, O(log n) runs instead of restoring snapshots one by one.

```bash
avc bisect --good snap-abc --cmd "go test ./..."
avc bisect --good-tag stable --bad snap-xyz --cmd "npm test"
avc bisect --branch feat/auth --good snap-abc --cmd "pytest -x" --json
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--good <id>` | Known-good snapshot ID (required unless `--good-tag`) |
| `--good-tag <tag>` | Use the newest snapshot with this tag as the good point |
| `--bad <id>` | Known-bad snapshot (default: branch HEAD) |
| `--branch <name>` | Branch to search (default: main) |
| `--cmd <command>` | Test command (required) |
| `--timeout <s>` | Per-step timeout (default: sandbox default) |

**Exit-code protocol** (same as `git bisect run`): `0` = good, `125` = skip
(cannot judge this snapshot, e.g. it does not build), anything else = bad.

Each candidate is materialized fresh into a throwaway scratch workspace
(`.avc/workspaces/.bisect-*`, removed afterwards) and the command runs
through the same sandbox as `avc run` — environment scrubbing, timeout,
output caps. **Requires `[run] enabled = true`** in `.avc/config.toml`,
because bisect executes arbitrary commands.

With `--json`, progress streams as one NDJSON object per step
(`{"type":"step",...}`) followed by a final `{"type":"result",...}` naming
the first bad snapshot, its predecessor, and a summary of what changed
between them. If skipped snapshots prevented exact narrowing the result is
flagged `"ambiguous": true`.

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
| `merged`   | Both sides changed **different regions** of the file — combined line-by-line automatically |
| `delete`   | The branch deleted a file main left unchanged — removed from main |
| `conflict` | Both sides changed the **same region** — only those hunks get conflict markers; the rest of the file merges. (Whole-file markers still apply to binary files, files over the diff size cap, and delete-vs-edit.) |
| `skip`     | No net change relative to the merge base — left untouched |

**Protected paths:** if `[protect]` is configured (see below), a merge that
would change a protected path is refused in `block` mode. A human can
override with `--allow-protected`; the MCP merge tool has no equivalent, so
agents cannot. Refusals happen before anything is written — no snapshot, no
merge record.

**JSON output:**
```json
{
  "merge_id": "merge-abc123",
  "branch": "feat/my-branch",
  "preview": false,
  "clean": 3,
  "merged": 1,
  "deleted": 1,
  "conflicts": 0,
  "skipped": 12,
  "files": [{ "path": "src/auth.go", "decision": "clean" }],
  "post_merge_snapshot_id": "snap-def456",
  "auto_snapshot_id": "snap-ghi789",
  "protected_changes": ["secrets/token.txt"],
  "protected_mode": "warn"
}
```

- `auto_snapshot_id` appears when the branch's workspace had changes since its last snapshot — those changes are captured automatically before the merge runs, so un-snapshotted work is never silently dropped. `--preview` never creates this snapshot (it has no side effects); instead its output includes `workspace_dirty_files` as a warning that the preview does not yet reflect those changes.
- A conflict between an edit and a deletion is written with a labeled diff3 marker (e.g. `>>>>>>> branch (theirs) — file deleted on branch`) so it's clear which side removed the file.
- Merge status values: `in_progress` → `completed` | `conflicts` | `failed` | `aborted`. A merge that fails partway through applying its plan is marked `failed` (not left stuck at `in_progress`), so `avc merge --abort` can always find and roll back the last attempt regardless of how it ended.

### `avc merge --train` — merge queue for agent fleets

Merge several branches into main in order, each against the *current* main —
so every merge sees the ones before it, instead of every preview going stale
the moment the first branch lands.

```bash
avc merge --train feat/a feat/b feat/c
avc merge --train feat/a feat/b --validate "go test ./..."
```

Per branch, in order:

1. Preview against current main. Conflicts or a `[protect]`-blocked change →
   the train **stops before writing anything**; that branch is reported and
   the rest are `skipped`. Main keeps the merges completed so far — each is
   individually reversible with `avc undo` or its pre-merge snapshot.
2. Clean → the full merge pipeline runs (dirty-workspace auto-snapshot,
   diff3 line merge, protected-path gate, pre/post-merge snapshots).
3. `--validate "<command>"` runs against post-merge main through the same
   sandbox as `avc run` (**requires `[run] enabled = true`**). A non-zero
   exit rolls exactly that merge back — pre-merge snapshot restored, branch
   active again, workspace rebuilt — and stops the train.

**JSON output:** per-branch results plus a train summary. The command exits
non-zero when the train stopped early.

```json
{
  "results": [
    {"branch": "feat/a", "status": "merged", "post_merge_snapshot_id": "snap-...", "clean": 2},
    {"branch": "feat/b", "status": "conflicts", "conflicts": 1, "detail": "src/auth.go"},
    {"branch": "feat/c", "status": "skipped"}
  ],
  "completed": 1,
  "stopped_at": "feat/b",
  "success": false
}
```

Statuses: `merged` · `conflicts` · `blocked_protected` · `validation_failed` · `error` · `skipped`.

---

## `avc branch create --from-branch` — stacked branches

Root a new branch at another branch's current HEAD instead of main's, so the
child starts from the parent's latest work:

```bash
avc branch create feat/api-tests --from-branch feat/api
```

Merging a child still targets main — its base snapshot already encodes the
fork point, so the three-way math is unchanged (merge the parent first, then
the child; a train handles the ordering naturally). The lineage is recorded
and shown by `avc branch list` (`(from feat/api)` and `parent_branch` in
`--json`).

---

## `avc branch diff <a>..<b>` — cross-branch diff

Compare two branches' HEAD snapshots — how two parallel lines of work
differ, rather than what one changed since its base:

```bash
avc branch diff feat/auth..feat/api
avc branch diff main..feat/auth --stat
```

Both sides accept any branch name, including `main`. The single-argument
form (`avc branch diff [branch]`) still shows a branch's cumulative
base→HEAD diff. MCP: `avc_branch_diff` accepts an optional `against`
argument for the same comparison.

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

### `avc trash restore <op_id> [path]`

Moves quarantined files back to their original location (recorded when they
were quarantined — project root or a branch workspace). Pass a relative path
to restore one file, or omit it for the whole entry. Files that already
exist at the destination are skipped, never overwritten.

```bash
avc trash restore 2026-07-09T15-04-05-restore-af2202              # everything
avc trash restore 2026-07-09T15-04-05-restore-af2202 notes.txt   # one file
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

## `avc undo`

Reverses the most recent restore or merge with zero arguments — the safety
snapshot AVC took before that operation is restored. Undoing a merge also
reactivates the merged branch and rebuilds its workspace. Every undo records
itself into the same operations log, so running `avc undo` twice acts as redo.

```bash
avc undo           # reverse the newest operation
avc undo --list    # show recent operations and what undo would reverse
avc undo --json
```

**JSON output:**
```json
{
  "undone_kind": "merge",
  "undone_details": "merged branch 'feat/x' into main",
  "restored_snapshot_id": "snap-abc123",
  "redo_snapshot_id": "snap-def456",
  "branch": "main",
  "reactivated_branch": "feat/x",
  "success": true
}
```

---

## `avc verify`

Re-hashes every object in the store and reports any whose content no longer
matches its content-addressed filename. The hot read path deliberately skips
this check (it would double restore cost) — verify is the explicit audit.
`avc fsck` works as an alias for git/unix muscle memory.

```bash
avc verify             # exits non-zero if corruption is found
avc verify --repair    # quarantine corrupt objects to .avc/corrupt/
avc verify --json
```

Each corrupt object is mapped to the snapshots that reference it, so you
know exactly which history is damaged. The non-zero exit on corruption makes
verify suitable as a CI or pre-backup gate.

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
- **Layering on a branch:** the project-root `.avcignore` is read fresh at snapshot time and always applies; a branch workspace's own `.avcignore` (e.g. one an agent added) is layered on top. So a root edit takes effect on live branches immediately, and workspace-specific patterns add to (or, by the last-match-wins rule, override) the root's. Because layering is additive, *relaxing* a pattern already present in the workspace copy — e.g. changing `vendor/` to `/vendor/` — also requires updating the workspace `.avcignore`, not only the root.
- **Ignoring never untracks.** Adding a pattern that matches an already-tracked file does **not** remove it: if the file still exists on disk it stays in snapshots (mirroring `git`, where `.gitignore` never untracks a tracked file). To stop tracking a file, delete it from the workspace. A snapshot reports how many such files it carried forward via `carried_files`.

**Default patterns (written by `avc init`):**
```
node_modules/
/vendor/       # anchored: only Go's module-root vendor dir, not a source dir named "vendor"
dist/
build/
.env
.DS_Store
```

The `.avc/` directory is always excluded regardless of `.avcignore` contents.

### Diagnosing exclusions — `avc check-ignore`

When an expected file is missing from a snapshot, branch workspace, or diff, use `avc check-ignore` to find out whether — and why — it is excluded. It is AVC's analog of `git check-ignore`, and it names the exact pattern responsible:

```
avc check-ignore web/features/vendor/screen.tsx
avc check-ignore --json src/main.go vendor/pkg/x.go
```

Paths are resolved against the active branch's source directory (the workspace on a branch, the project root on main), using the same layered rules a snapshot sees. Exit code is `0` when at least one given path is ignored and `1` when none are, matching `git check-ignore`.

### Large files

Files larger than `[snapshot] max_file_size_mb` in `.avc/config.toml` (default
100 MB) are skipped — not read, hashed, or stored — with a warning printed to
stderr and listed in the snapshot's `skipped_large` field. This protects
against an out-of-memory read on an accidentally-tracked large binary.

---

## `[protect]` — protected paths

List paths agents must not change under `[protect]` in `.avc/config.toml`.
Globs use `.avcignore` syntax (`**`, trailing `/` for directories):

```toml
[protect]
paths = [".github/workflows/**", "secrets/**", "*.pem"]
mode  = "block"   # or "warn"
```

- **Enforcement is mechanical, at merge time.** In `block` mode (the default
  — an unset or misspelled mode fails safe to block), a merge that would
  change a protected path is refused before anything is written. In `warn`
  mode it proceeds with a prominent warning.
- **Only a human can override**, with `avc merge <branch> --allow-protected`
  from a terminal. The MCP merge tool has no override parameter, so an agent
  cannot lift the gate — the same trust model as `[run] enabled`.
- **Early warnings everywhere:** `avc status` marks changed protected files
  with `!`, and the MCP snapshot/status/branch-diff responses include a
  `protected_changes` list so agents can surface the collision to the user
  long before merge time.
- A malformed `config.toml` fails closed: the merge is refused rather than
  silently proceeding without the protection rules.

Protection applies to AVC-mediated integration (merges into main). An agent
writing directly to the project root on main bypasses it — one more reason
the agent instructions mandate branch workspaces.

---

## Object store format

Objects in `.avc/objects/` are stored zstd-compressed when compression saves
space (a 13-byte `AVCO` header records the original size), and as raw bytes
otherwise. Objects written by older AVC versions remain readable — the two
forms coexist freely. `avc storage` reports the on-disk vs. logical size and
how many objects are compressed; `avc verify` checks both forms.
