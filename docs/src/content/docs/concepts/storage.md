---
title: Storage Layout
description: How AVC organizes data on disk — SQLite metadata, content-addressed object store, workspace directories.
---

Everything AVC needs lives inside a single `.avc/` directory at the root of your project. No global config, no remote storage, no daemon.

## The `.avc/` layout

```
.avc/
├── avc.db                  # SQLite — all metadata (no file bytes)
├── config.toml             # Project config (active branch, default agent, etc.)
├── .gitignore              # Tells Git to ignore the .avc/ directory
├── stat-cache.json         # mtime+size cache for fast incremental snapshots
├── objects/                # Content-addressed file blobs
│   ├── ab/
│   │   └── cdef0123...     # SHA256 hash, sharded by first 2 hex chars
│   └── ff/
│       └── 0011223344...
└── workspaces/             # Branch workspaces (only for non-main branches)
    ├── feat-auth/
    │   ├── src/            # hardlinked from project root initially
    │   └── README.md
    └── feat-payments/
        └── ...
```

## SQLite metadata

The database holds **no file bytes** — only hashes, sizes, and relational metadata.

| Table | Purpose |
|-------|---------|
| `projects` | One row per AVC-initialized project (the project root path) |
| `branches` | All branches with their base snapshot, active flag |
| `snapshots` | Snapshot metadata: label, agent, timestamp, branch, file count, total size |
| `files` | Per-snapshot file list: relative path, file hash, file size |
| `diffs` | Cached diff results (regenerated if missing) |
| `merges` | One row per merge attempt: branch, target, pre-merge safety snapshot |
| `merge_files` | Per-file decisions inside a merge: `clean` / `conflict` / `skip` |

The schema is migration-aware. Upgrading AVC may add columns or tables; data is preserved.

## Content-addressed object store

File blobs are stored in `.avc/objects/<hash[:2]>/<hash[2:]>` — the same sharding scheme used by Git. Properties:

- **Deduplication** — identical files across snapshots share **one** object on disk
- **Immutability** — objects are write-once. Restoring an old snapshot reads the original bytes verbatim
- **Cheap snapshots** — only changed files produce new objects. A 1000-file project with one changed file adds exactly one object to the store
- **Sharded** — first two hex chars create a directory level, avoiding the "too many files in one directory" filesystem problem at scale

Example: if your `README.md` has SHA256 hash `abcd1234...ef`, it's stored at:

```
.avc/objects/ab/cd1234...ef
```

A snapshot row references this hash. Multiple snapshots referencing the same hash share the single stored object.

## Workspaces

`main` operates on the real project root directly — no workspace. Every other branch gets a materialized workspace at `.avc/workspaces/<branch-name>/`.

Workspaces are populated from the branch's base snapshot using **hardlinks** (sharing inodes with the real project root). When the agent modifies a file, the OS copies-on-write — the original inode in the project root is untouched.

If hardlinks aren't available (cross-device, restricted permissions), AVC falls back to a regular file copy. The workspace still works; it just uses more disk space.

## Stat cache

`.avc/stat-cache.json` makes incremental snapshots fast. After a snapshot:

```json
{
  "snapshot_id": "snap-abc123",
  "files": {
    "src/auth/login.go":   { "mtime_ns": 1745101234567890, "size": 4096, "hash": "abc..." },
    "src/auth/session.go": { "mtime_ns": 1745101234500000, "size": 2048, "hash": "def..." }
  }
}
```

On the next snapshot, AVC checks each file's `mtime + size` against the cache. If they match, the cached hash is reused — no read, no hash computation. Unchanged files are effectively free.

The cache is corruption-safe: if it's missing or invalid, AVC just re-hashes everything (slower, but never wrong).

## Telling Git to ignore `.avc/`

AVC writes `.avc/.gitignore` automatically. Inside `.avc/`:

```
*
```

This is the simplest possible ignore — ignore everything inside this directory. If you keep your project in Git, the entire `.avc/` directory stays out of your Git history.

If your project's root `.gitignore` doesn't already have `.avc/`, `avc init` appends it for you.

## Per-project config

`.avc/config.toml` is small and rarely edited by hand:

```toml
[branch]
active = "main"

[defaults]
agent = ""
```

The active branch is updated by `avc branch switch`. Everything else is optional.

## What lives outside `.avc/`

- `.avcignore` at the project root — patterns excluded from snapshots (`.gitignore` syntax)
- Agent integration files (only if you run `avc init --skills <framework>`):
  - `.claude/settings.json`, `.claude/skills/avc-*/SKILL.md`
  - `.cursor/mcp.json`, `.cursor/rules/avc.mdc`
  - `.codeium/windsurf/mcp_config.json`, `.windsurfrules`

These are deliberately outside `.avc/` because they're configuration the agent framework reads, not AVC's own state.

## Backup strategy

`.avc/` is your snapshot store. To back it up, just copy the directory:

```bash
tar -czf project-backup.tgz project/
```

This includes the project files, the AVC database, and the object store. Restoring is a straight `tar -xzf` — no AVC-specific restore process needed.

For long-term archival, treat `.avc/` like any other project asset.
