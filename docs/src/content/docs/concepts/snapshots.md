---
title: Snapshots
description: The core unit of AVC — content-addressed, immutable, point-in-time captures.
---

A **snapshot** is AVC's atomic unit: a complete, content-addressed picture of your project at one moment. Snapshots are immutable, deduplicated, and reversible.

## What a snapshot contains

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique ID, e.g. `snap-a1b2c3d4` |
| `label` | string | Human-readable description you provide |
| `timestamp` | int | Unix epoch when the snapshot was taken |
| `agent_name` | string | Optional — which agent/user created it |
| `notes` | string | Optional free-form notes |
| `files` | list | Every tracked file with its SHA256 hash and size |
| `branch_id` | string | Which branch the snapshot belongs to |

The snapshot does **not** contain file bytes directly — they live in the content-addressed object store and are referenced by hash. See [Storage Layout](/agentic-vc/concepts/storage/) for the details.

## How snapshots are created

When you run `avc snapshot "label"`:

1. AVC walks the project directory, respecting `.avcignore`
2. Every tracked file is read and SHA256-hashed
3. New objects (files whose hashes don't exist in the store yet) are written to `.avc/objects/<hash[:2]>/<hash[2:]>`
4. A new `snapshots` row is inserted in SQLite with the metadata
5. One `files` row per tracked file links the snapshot to the object hashes

If a file hasn't changed since the previous snapshot, **no new object is written** — both snapshots reference the same blob. This is content-addressed deduplication, the same scheme Git uses.

## Performance

Subsequent snapshots on unchanged projects are nearly instantaneous thanks to two optimisations:

- **`ReadAndHash`** — files are read once and hashed in the same pass
- **Stat cache** — `.avc/stat-cache.json` stores `(path → mtime + size + hash)`. On the next snapshot, files whose mtime and size haven't changed are skipped entirely

A typical project of 1,000 files where nothing has changed snapshots in well under 100 ms.

## Immutability

Once created, a snapshot's contents cannot be modified. You can:

- **Read** it (via `avc info`, `avc cat`, `avc diff`, `avc restore`)
- **Delete** it (via `avc delete`) — removes the metadata + any objects no longer referenced
- **Reference** it (as a base for a branch, a diff endpoint, etc.)

You cannot edit a snapshot. To change what's captured, take a new snapshot.

## What's excluded

Two layers of filtering:

1. **Hard-coded** — `.avc/`, `.git/`, `.hg/`, `.svn/`, `.bzr/` are always excluded
2. **`.avcignore`** — a `.gitignore`-style file in the project root. AVC writes a default one on `avc init` covering common build artefacts, dependencies, secrets, and OS metadata

You can edit `.avcignore` at any time. It's reread on every snapshot.

## What snapshots are not

- **Not** Git commits — there's no commit graph, no parent pointers (yet), no signing
- **Not** backups — they live alongside your project in `.avc/`. If you `rm -rf` the project, the snapshots go with it. For real backup, use a separate tool
- **Not** versioned across machines — purely local
- **Not** point-in-time backups of file metadata — they store content, hash, size, and relative path, but not mtime, permissions, or ACLs

## Working with snapshots

| Action | Command |
|--------|---------|
| Create | `avc snapshot "label"` |
| List on active branch | `avc list` |
| View metadata + file list | `avc info <id>` |
| Print file contents from a snapshot | `avc cat <id> <path>` |
| Diff against another snapshot | `avc diff <from> <to>` |
| Diff against working tree | `avc diff-current <id>` |
| Restore | `avc restore <id>` |
| Restore one file | `avc restore-file <id> <path>` |
| Delete | `avc delete <id>` |
| Show every snapshot containing a file | `avc file-history <path>` |
| Line-by-line attribution | `avc annotate <path>` |

See the [CLI Reference](/agentic-vc/cli/) for full details.
