# Plan 01 — Data Safety

**Covers review items:** 1.1, 1.4, 2.1, 2.2, 2.6
**Goal:** After this plan, no AVC operation can destroy data the user did not ask it to touch, and no crash can corrupt stored history.
**Estimated duration:** ~1 week

---

## 1 · Restore must not delete ignored/untracked files (review 1.1)

**Priority:** P0 · **Effort:** M
**Files:** `avc/internal/restore/restore.go:84-104`, `avc/internal/fileutil/fileutil.go`, new `avc/internal/trash/trash.go`

### Problem

`RestoreToDir` removes every file under `targetDir` that is not in the target snapshot. It never loads `.avcignore`, so ignored files (`.env`, `node_modules/`, local DBs) are deleted on every restore. Verified by reproduction (review §1.1).

### Implementation

**Step 1 — respect ignore rules in the deletion sweep.**

```go
ignore, err := fileutil.LoadIgnoreRules(projectRoot)
if err != nil {
    return nil, fmt.Errorf("load ignore rules: %w", err)
}

_ = filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
    // ... existing .avc/.git skip ...
    rel = filepath.ToSlash(rel)
    if d.IsDir() {
        if ignore.MatchesDir(rel) {
            return filepath.SkipDir       // never descend into ignored dirs
        }
        return nil
    }
    if ignore.Matches(rel) {
        return nil                        // ignored file — never touch it
    }
    if !targetPaths[rel] {
        _ = trash.Move(projectRoot, targetDir, rel)   // Step 2, not os.Remove
    }
    return nil
})
```

The skip logic must mirror `fileutil.WalkProject` exactly — extract a shared helper if they drift.

**Step 2 — quarantine instead of delete (defense in depth).**

New package `internal/trash`:

```go
// Move relocates targetDir/rel to .avc/trash/<opID>/<rel>, creating parents.
// opID is a timestamped identifier (e.g. "2026-07-09T15-04-05-restore").
func Move(projectRoot, targetDir, rel string) error
// List returns trash entries grouped by opID, newest first.
func List(projectRoot string) ([]Entry, error)
// Empty removes trash entries older than the given age (0 = all).
func Empty(projectRoot string, olderThan time.Duration) error
```

CLI surface: `avc trash list`, `avc trash empty [--older-than 7d]`, both with `--json`. Auto-empty entries older than 7 days at the end of any restore (best-effort, stderr note). Full restore-from-trash UX is deferred to Plan 04 (A3) — this plan only guarantees nothing is unrecoverable.

**Step 3 — regression test** reproducing the review scenario: `.avcignore` with `*.env`, create `prod.env`, snapshot, restore, assert `prod.env` still exists. Second test: untracked-but-not-ignored file ends up in trash, not gone.

---

## 2 · Never hardlink into workspaces (review 1.4)

**Priority:** P0 · **Effort:** S
**Files:** `avc/internal/branch/copyopt.go`, `avc/internal/branch/branch.go:56-101`

### Problem

`copyFileOptimized` tries `os.Link` first. Workspace files then share inodes with project-root files; any in-place write in the workspace (append, `sed -i`, editor save) mutates the real project root. Verified by reproduction (review §1.4). This breaks the core isolation guarantee.

### Implementation

Delete the hardlink attempt — `copyFileOptimized` becomes `regularCopy` directly (keep the function name and signature so future reflink build-tag files slot in, per the existing comment):

```go
// copyFileOptimized copies src to dst. Hardlinks are deliberately NOT used:
// workspace files must never share inodes with project-root files, because
// in-place writes in the workspace would mutate the original (data corruption).
// Reflink (true copy-on-write) is safe and can be added per-platform later.
func copyFileOptimized(src, dst string) error {
    return regularCopy(src, dst)
}
```

Update the now-wrong comment block on `copyToWorkspace` (branch.go:56-68). Remove `tryHardlink` (dead code rule).

**Test:** create branch with no base snapshot, append to a workspace file via `os.OpenFile(..., os.O_APPEND, ...)`, assert the project-root file is unchanged.

*Perf note:* the reflink upgrade (FICLONE / clonefile / FSCTL_DUPLICATE_EXTENTS_TO_FILE) stays on the backlog as originally planned in improvement-plan.md §8.2 — reflink is safe because the OS breaks the share on write.

---

## 3 · Atomic object-store writes (review 2.1)

**Priority:** P0 · **Effort:** S
**Files:** `avc/internal/restore/restore.go:177-186`

### Problem

`StoreObject` uses a bare `os.WriteFile`. A crash or disk-full mid-write leaves a truncated blob, and the `os.Stat` existence check then treats it as stored forever — all future snapshots dedupe against the corrupt object.

### Implementation

Temp-file + rename, same pattern the statcache already uses:

```go
func StoreObject(projectRoot, hash string, data []byte) error {
    path := objectPath(projectRoot, hash)
    if _, err := os.Stat(path); err == nil {
        return nil
    }
    if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
        return err
    }
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, data, 0644); err != nil {
        return err
    }
    return os.Rename(tmp, path)
}
```

Also guard the read side against the empty-hash panic (shared root cause with review 1.2 — the merge-side fix lands in Plan 02, but the guard belongs here):

```go
func readObject(projectRoot, hash string) ([]byte, error) {
    if len(hash) < 3 {
        return nil, fmt.Errorf("invalid object hash %q", hash)
    }
    ...
}
```

Apply the same guard to `diff.ReadObjectSafe` (`diff.go:137-144`). Have GC delete stray `*.tmp` files in the objects tree (one-line addition to the `WalkDir` in `gc.go`).

Full verify-on-read / `avc fsck` is Plan 04 (A4).

**Test:** simulate a torn write by pre-creating `<objectPath>` content mismatched to its hash — out of scope until fsck; here, test that a `.tmp` file left behind never masks the real object and that `StoreObject` after a failed run completes correctly.

---

## 4 · Transactional snapshot insert (review 2.2)

**Priority:** P0 · **Effort:** S
**Files:** `avc/internal/db/db.go`, `avc/internal/snapshot/snapshot.go:169-174`

### Problem

`InsertSnapshot` and `InsertFilesBatch` run in separate transactions. A crash between them leaves a snapshot row with zero file rows — which a later restore interprets as "delete every tracked file" (compounding item 1).

### Implementation

Add a combined method and use it in `snapshot.Create`:

```go
// InsertSnapshotWithFiles persists the snapshot row and all its file rows in
// a single transaction, so a partially-written snapshot can never exist.
func (s *Store) InsertSnapshotWithFiles(snap *Snapshot, files []*File) error {
    tx, err := s.db.Begin()
    if err != nil { return err }
    // snapshot insert, then prepared-statement file loop (reuse InsertFilesBatch body)
    // rollback on any error; single commit
}
```

Keep `InsertSnapshot`/`InsertFilesBatch` only if other callers remain (archive import uses its own path — check and consolidate); otherwise delete them (dead-code rule).

**Test:** error injection — wrap a failing file row (e.g. duplicate PK) and assert the snapshot row is absent afterwards.

---

## 5 · `busy_timeout` pragma (review 2.6)

**Priority:** P0 · **Effort:** S
**Files:** `avc/internal/db/db.go:120-132`

### Problem

WAL permits readers during a write, but concurrent *writers* fail instantly with `SQLITE_BUSY`. AVC routinely has four writers (CLI, MCP server, extension, web UI).

### Implementation

Add to the pragma list in `Open`:

```go
"PRAGMA busy_timeout=5000", // writers wait up to 5 s instead of failing on SQLITE_BUSY
```

**Test:** two goroutines each doing open→insert-snapshot→close in a loop against the same project for ~2 s; assert zero `database is locked` errors. (Note: this exercises cross-connection contention, which works without CGO because each `db.Open` is its own connection pool.)

---

## Exit criteria

- [x] Regression test: restore preserves `.avcignore`-matched files (review repro as test)
- [x] Untracked non-ignored files removed by restore land in `.avc/trash/`, listable via `avc trash list --json`
- [x] Workspace files never share inodes with project-root files (append test)
- [x] `tryHardlink` removed; no references remain
- [x] Object store contains no path written without temp+rename; GC sweeps stray `.tmp`
- [x] `readObject`/`ReadObjectSafe` return an error (not panic) on hashes shorter than 3 chars
- [x] Snapshot row + file rows are atomic (error-injection test)
- [x] Concurrent-writer test passes with `busy_timeout=5000`
- [x] `go test ./...` green; `docs/cli-reference.md` documents `avc trash`

### Implementation notes (beyond the original plan)

- `objectPath`'s empty-hash guard was placed directly in `objectPath` (returns `""`) in addition to `readObject`/`StoreObject`, so any accidental call site is protected, not just the two known ones.
- **Two additional real concurrency bugs surfaced by the busy_timeout regression test** (`TestDB_ConcurrentSnapshots_NoBusyErrors`), both fixed:
  1. `busy_timeout` must be the *first* pragma applied in `Open` — `journal_mode=WAL` itself needs exclusive access, so applying it before `busy_timeout` left that very first pragma unprotected under contention.
  2. `StoreObject`'s temp-file suffix must be unique per *call*, not per *process* — concurrent goroutines share a PID, so a PID-only suffix collided under concurrent identical-content writes. Fixed with a random suffix plus a bounded rename retry (Windows can surface "access denied" when two renames race onto the same content-addressed destination, even though the outcome — byte-identical content — is fine either way).
