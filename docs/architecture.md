# AVC Architecture

## Overview

AVC is built around four primitives that together make agent-assisted development safe: **commit**, **branch**, **diff**, and **merge**. The VSCode extension is a thin UI shell; all logic lives in the Go CLI binary. Phases 1–3 deliver commit and diff; Phases 4–5 deliver branch and merge.

Allowing an agent to do real work introduces nondeterminism — you can no longer make hard guarantees about project state. Each primitive addresses one dimension of that problem:

| Primitive | What it solves | AVC mechanism |
|-----------|---------------|---------------|
| **Commit** | Changes must be auditable and reversible | Snapshots + content-addressed object store |
| **Diff** | It must be clear what the agent changed | `avc diff` / diff viewer |
| **Branch** | Agents need isolated workspaces that can't corrupt main | `avc branch` (Phase 4) |
| **Merge** | Approved agent work must flow back to main safely | `avc merge` (Phase 5) |

AVC follows a CLI-first, layered architecture.

```
┌─────────────────────────────────────────────────────┐
│                   VSCode Extension                   │
│  (Snapshot list, diff viewer, restore UI, settings)  │
└────────────────────┬────────────────────────────────┘
                     │ subprocess + JSON (stdout/stderr)
┌────────────────────▼────────────────────────────────┐
│           CLI Tool  (avc)                            │
│  init · snapshot · list · diff · restore · info     │
└────────────────────┬────────────────────────────────┘
                     │ function calls
┌────────────────────▼────────────────────────────────┐
│          Core Engine  (Go internal packages)         │
│  db · snapshot · restore · diff · fileutil · config │
└────────────────────┬────────────────────────────────┘
                     │ reads / writes
         ┌───────────▼──────────────┐
         │  .avc/  project store    │
         │  ├── avc.db  (SQLite)    │
         │  ├── config.toml         │
         │  ├── .gitignore          │
         │  └── objects/            │
         │      └── <sha256-shards> │
         └──────────────────────────┘
```

---

## Layer responsibilities

### VSCode Extension (`extension/`)

Communicates with the CLI exclusively via `child_process.execFile`. It never touches the database or object store directly.

- **`extension.ts`** — activates the extension, registers commands
- **`sidebar.ts`** — `TreeDataProvider` that renders the snapshot list
- **`diffViewer.ts`** — builds a Webview HTML page from diff JSON
- **`cliProxy.ts`** — typed wrappers around every `avc` command; handles `--json` parsing and error propagation

### CLI (`cmd/avc/`)

One file per subcommand. Each command:
1. Parses flags with Cobra
2. Calls one or more `internal/` packages
3. Prints either human-readable text or `--json` output to stdout
4. Exits non-zero and writes to stderr on any error

### Core Engine (`internal/`)

| Package | Responsibility |
|---------|---------------|
| `db` | SQLite schema, migrations, all CRUD — `projects`, `snapshots`, `files`, `diffs` tables |
| `fileutil` | SHA256 hashing, directory walk, `.avcignore` pattern matching |
| `snapshot` | Orchestrates a snapshot: walk → hash → store objects → insert DB records; generates heuristic change summaries |
| `restore` | Reads file blobs from the object store and writes them back to the working tree |
| `diff` | Compares two snapshots by joining file maps; computes line counts from stored blobs; per-file change summaries |
| `config` | Reads/writes `.avc/config.toml` (TOML, `github.com/BurntSushi/toml`) |
| `watch` | `avc watch` daemon: fsnotify-based (or polling) debounced continuous checkpointing, deduped against branch HEAD |
| `timeline` | Groups a branch's snapshots by agent session and interleaves operations-log events — the `avc timeline` report |
| `bisect` | Binary search over snapshot history for the first snapshot that breaks a command, in a scratch workspace |

### Continuous checkpointing (`avc watch`)

`avc watch` makes safety structural instead of behavioral: agents are asked
to snapshot before every change, but the watcher guarantees recoverability
even when they don't. Change events (fsnotify; polling fallback for network
filesystems) mark a target tree — the project root or a branch workspace —
dirty; after a debounce quiet period the tree is compared to the branch HEAD
using the stat cache and snapshotted only if it differs. Checkpoints carry
the `auto:watch` label prefix and their own retention cap
(`max_watch_snapshots_per_branch`, default 200, pruned before any other
rule), so continuous checkpointing can never crowd out deliberate snapshots.
A pid file with a heartbeat mtime enforces one watcher per project without
platform-specific process probing.

One fsnotify subtlety is load-bearing: the daemon's main loop registers new
directories with `watcher.Add` while events are flowing, and on some
platforms the fsnotify backend blocks event delivery while servicing `Add` —
consuming events on the same goroutine would deadlock. A pump goroutine
therefore drains `watcher.Events` into a buffered queue; on overflow it
degrades to "mark every target dirty" (a cheap re-check) rather than losing
a change.

---

## Storage layout

```
.avc/
├── avc.db              # SQLite — metadata only (no file contents)
├── config.toml         # Per-project settings
├── .gitignore          # Tells Git to ignore the .avc/ directory
└── objects/
    ├── ab/
    │   └── cdef0123…   # File blob addressed by SHA256 hash
    └── ff/
        └── 00112233…
```

### Content-addressed object store

File blobs are stored in `.avc/objects/<first-2-hex>/<remaining-62-hex>` — the same sharding scheme used by Git. This provides:

- **Deduplication** — identical files across snapshots share one object on disk.
- **Immutability** — objects are write-once; restoring a snapshot is always a pure read.
- **Cheap snapshots** — only changed files produce new objects; unchanged files cost nothing beyond a DB row.

The `internal/objstore` package is the single owner of the store — all reads
and writes (from `snapshot`, `restore`, `diff`, `merge`, `fsck`) go through it.

**On-disk object format (v2).** Each object is one of two forms, detected by
prefix on read:

- *Compressed*: a 13-byte header — magic `AVCO`, format byte `0x01`, 8-byte
  little-endian raw size — followed by one zstd frame. Written only when
  compression actually saves space.
- *Raw*: the exact original bytes, headerless. Every object written before
  compression existed is this form, as is content that doesn't compress.

Anything that fails to parse as a well-formed compressed object (including
the pathological legacy file whose own content starts with the magic) falls
back to raw bytes, so the two forms coexist indefinitely with no migration.
Writes are atomic (unique temp file + rename). `avc verify` re-hashes every
object to audit integrity; the hot read path deliberately does not.

### SQLite schema

```sql
projects  (id, path, name, created_at)
snapshots (id, project_id, timestamp, label, agent_name, notes, file_count, total_size)
files     (id, snapshot_id, relative_path, file_hash, file_size)
diffs     (id, from_snapshot_id, to_snapshot_id, file_path, diff_type, old_hash, new_hash, change_summary)
```

The `diffs` table is a **cache** — it is populated on first request and reused on subsequent calls. It can be invalidated without data loss.

---

## Extension ↔ CLI protocol

All data exchange uses newline-delimited JSON on stdout. Errors go to stderr; the process exits with a non-zero code.

```
Extension                          CLI process
   │                                   │
   │── execFile('avc', ['list','--json']) ──>│
   │                                   │
   │<── stdout: JSON array ────────────│
   │    (or stderr + exit 1 on error)  │
```

Each invocation is **stateless** — no daemon, no socket, no shared memory.

---

## Key design decisions

| Decision | Rationale |
|----------|-----------|
| CLI-first | Agents and scripts can use AVC without a UI |
| Subprocess + JSON | No language barrier, no port management, easy testing |
| Content-addressed objects | Deduplication, immutability, cheap snapshots |
| SQLite (no server) | Zero setup, single file, works offline |
| Linear main + shallow agent branches | Simple mental model for users; branches exist only as agent workspaces, not general-purpose |
| Merge auto-snapshots main first | Every merge is preceded by a snapshot of main, so it is always reversible |
| `.avcignore` | Users control what is tracked; `node_modules` etc. excluded by default |
| No auto-triggers | Explicit snapshots give users and agents full control |
