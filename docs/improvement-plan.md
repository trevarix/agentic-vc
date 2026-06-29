# AVC Improvement Plan — Phased Implementation

**Date:** 2026-05-23  
**Status:** Proposed  
**Source:** Full codebase review of Phases 1–7  
**Phases:** 8 (each builds on the previous; do not start a phase until the prior one passes its exit criteria)

---

## Reading Guide

Each phase follows this structure:

- **Goal** — the single outcome that defines "done" for the phase
- **Deliverables** — the ordered list of items to implement
- **Per-item detail** — problem, affected files, concrete implementation, effort
- **Exit criteria** — the checklist that must be green before moving to the next phase

Effort key: **S** < 4 h · **M** 1–3 days · **L** 3–7 days · **XL** 1–2 weeks

---

## Phase 1 — Database & Correctness Foundation

**Goal:** Fix the silent data-correctness and concurrency problems that exist today, so every subsequent phase builds on a reliable base.

**Why first:** These are load-bearing fixes. Indexes affect every query in every later phase. The branch-name validation prevents a security class of bugs. The race condition affects multi-agent workflows. None of the later phases should be started on a foundation with these open.

**Estimated duration:** ~1 week

---

### 1.1 · SQLite WAL Mode + Query Indexes

**Priority:** P0 · **Effort:** S  
**Files:** `avc/internal/db/db.go:109`

#### Problem

SQLite defaults to rollback-journal mode: every write blocks all concurrent readers. The most common queries — `GetSnapshotFiles` (called in annotate, diff, merge, restore) and `ListSnapshotsByBranch` (called in list and the web server) — do full table scans. On a project with 100 snapshots and 500 files each, `GetSnapshotFiles` scans 50,000 rows per call. There are no indexes on any of the hot columns.

#### Implementation

**Step 1 — Apply pragmas in `Open()` before `migrate()`:**

```go
func Open(projectRoot string) (*Store, error) {
    path := filepath.Join(projectRoot, dbFile)
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, fmt.Errorf("open database: %w", err)
    }

    // Apply pragmas before schema migrations.
    pragmas := []string{
        "PRAGMA journal_mode=WAL",       // concurrent reads during writes
        "PRAGMA synchronous=NORMAL",     // safe + faster than FULL
        "PRAGMA cache_size=-65536",      // 64 MB page cache
        "PRAGMA foreign_keys=ON",        // enforce FK constraints
    }
    for _, p := range pragmas {
        if _, err := db.Exec(p); err != nil {
            db.Close()
            return nil, fmt.Errorf("pragma %q: %w", p, err)
        }
    }

    s := &Store{db: db}
    if err := s.migrate(); err != nil {
        db.Close()
        return nil, err
    }
    return s, nil
}
```

**Step 2 — Add indexes to `migrate()` (all idempotent via `IF NOT EXISTS`):**

```sql
CREATE INDEX IF NOT EXISTS idx_files_snapshot_id
    ON files(snapshot_id);

CREATE INDEX IF NOT EXISTS idx_files_path
    ON files(relative_path);

CREATE INDEX IF NOT EXISTS idx_snapshots_branch_ts
    ON snapshots(branch_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_merge_files_merge_id
    ON merge_files(merge_id);

CREATE INDEX IF NOT EXISTS idx_branches_project
    ON branches(project_id, name);
```

#### Verification

Run `EXPLAIN QUERY PLAN SELECT ... FROM files WHERE snapshot_id = ?` before and after. Confirm transition from `SCAN files` to `SEARCH files USING INDEX idx_files_snapshot_id`.

---

### 1.2 · Branch Name Validation

**Priority:** P0 · **Effort:** S  
**Files:** `avc/internal/branch/branch.go:106`, `avc/internal/mcp/handlers.go`

#### Problem

Branch names become directory names under `.avc/workspaces/<name>/`. A name like `../../etc/passwd`, one containing `:` or `*`, or a Windows reserved name like `con` or `nul` causes filesystem corruption or path traversal. The only validation today is `name == "main"`.

#### Implementation

Add `internal/branch/validate.go`:

```go
package branch

import (
    "fmt"
    "regexp"
    "strings"
)

// validBranchNameRe allows: letters, digits, -, _, /, .
// Mirrors Git's branch naming rules (minus the refs/ prefix).
var validBranchNameRe = regexp.MustCompile(`^[a-zA-Z0-9._/\-]{1,100}$`)

// windowsReserved are Windows device names that cannot be used as dir names.
var windowsReserved = map[string]bool{
    "con": true, "prn": true, "aux": true, "nul": true,
    "com1": true, "com2": true, "com3": true, "com4": true,
    "lpt1": true, "lpt2": true, "lpt3": true,
}

// ValidateBranchName returns a non-nil error if name is illegal.
func ValidateBranchName(name string) error {
    if name == "" {
        return fmt.Errorf("branch name cannot be empty")
    }
    if windowsReserved[strings.ToLower(name)] {
        return fmt.Errorf("'%s' is a reserved system name", name)
    }
    if !validBranchNameRe.MatchString(name) {
        return fmt.Errorf("branch name '%s' contains illegal characters; "+
            "use only letters, digits, -, _, /, and .", name)
    }
    if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
        return fmt.Errorf("branch name must not start or end with '.'")
    }
    if strings.Contains(name, "..") {
        return fmt.Errorf("branch name must not contain '..'")
    }
    return nil
}
```

Call `ValidateBranchName(name)` as the first statement in `branch.Create()` and in the `toolBranchCreate` MCP handler before any other work.

---

### 1.3 · `SetActiveBranch` Race Condition Fix

**Priority:** P0 · **Effort:** S  
**Files:** `avc/internal/config/config.go:85`

#### Problem

`SetActiveBranch` does a read-modify-write on `config.toml` with no synchronisation. Two agents running `avc branch create` concurrently (which auto-switches) will race — one write silently clobbers the other. The resulting active branch is whichever process won the OS write race.

#### Implementation

Replace the body of `SetActiveBranch` with a file-locked version:

```go
// SetActiveBranch atomically updates the active branch in config.toml
// using a lock file to prevent concurrent-write corruption.
func SetActiveBranch(projectRoot, name string) error {
    lockPath := filepath.Join(projectRoot, ".avc", "config.lock")
    lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
    if err != nil {
        return fmt.Errorf("open config lock: %w", err)
    }
    defer lock.Close()

    if err := lockFile(lock); err != nil {
        return fmt.Errorf("acquire config lock: %w", err)
    }
    defer unlockFile(lock)

    cfg, err := Load(projectRoot)
    if err != nil {
        c := defaultConfig
        cfg = &c
    }
    cfg.Branch.Active = name
    return Save(projectRoot, cfg)
}
```

Add platform-specific `lockFile` / `unlockFile` helpers:

- **Unix** (`config_unix.go`): `syscall.Flock(int(f.Fd()), syscall.LOCK_EX)` / `LOCK_UN`
- **Windows** (`config_windows.go`): `LockFileEx` / `UnlockFileEx` via `golang.org/x/sys/windows`

---

### 1.4 · diff3-Style Conflict Markers

**Priority:** P1 · **Effort:** S  
**Files:** `avc/internal/merge/merge.go:344`

#### Problem

Current conflict markers show only `main` vs `branch`. Without the base (common ancestor) content, the user cannot tell what either side _changed from_. This is the most important context for manual resolution. All three hashes are already present in `FileResult` — this is purely a rendering fix.

#### Implementation

Replace `writeConflict`:

```go
func writeConflict(projectRoot, dest string, f FileResult) error {
    read := func(hash string) []byte {
        if hash == "" {
            return nil
        }
        data, _ := restore.ReadObject(projectRoot, hash)
        return data
    }
    ensureNewline := func(b []byte) []byte {
        if len(b) > 0 && b[len(b)-1] != '\n' {
            return append(b, '\n')
        }
        return b
    }

    var sb strings.Builder
    sb.WriteString("<<<<<<< main (ours)\n")
    sb.Write(ensureNewline(read(f.MainHash)))
    sb.WriteString("||||||| base (common ancestor)\n")
    sb.Write(ensureNewline(read(f.BaseHash)))
    sb.WriteString("=======\n")
    sb.Write(ensureNewline(read(f.BranchHash)))
    sb.WriteString(">>>>>>> branch (theirs)\n")

    return fileutil.WriteFile(dest, []byte(sb.String()))
}
```

---

### Phase 1 — Exit Criteria

- [ ] `go test ./...` passes with no failures
- [ ] `EXPLAIN QUERY PLAN` on `GetSnapshotFiles` shows index usage
- [ ] `avc branch create "../../evil"` returns a clear error, creates no directory
- [ ] `avc branch create "con"` returns a clear error on Windows
- [ ] Concurrent `SetActiveBranch` calls from two goroutines do not corrupt config.toml (add a test)
- [ ] Conflict markers on a merge include `||||||| base` block
- [ ] `docs/cli-reference.md` updated if any user-visible behaviour changed

---

## Phase 2 — Storage Management

**Goal:** Give users and agents full control over disk usage — the ability to inspect it, reclaim it automatically, and bound it with a policy.

**Why now:** Phase 1 fixed correctness. Phase 2 ensures AVC doesn't silently accumulate unbounded storage as agents work. This must be done before Phase 8 (release) because storage bloat is one of the first things users notice.

**Estimated duration:** ~1 week

---

### 2.1 · Cascade Delete Snapshots on Branch Delete

**Priority:** P0 · **Effort:** S  
**Files:** `avc/internal/db/db.go:315`, `avc/internal/branch/branch.go:199`

#### Problem

`DeleteBranch` removes the branch record but leaves all snapshot rows with `branch_id` pointing to the deleted branch. These orphaned rows are invisible in `avc list`, cannot be restored, and block accurate storage accounting.

#### Implementation

Add to `db.go`:

```go
// DeleteSnapshotsByBranch removes all snapshot and file records for a branch.
// Objects in the object store are NOT removed — call gc.Run afterwards.
func (s *Store) DeleteSnapshotsByBranch(branchID string) error {
    tx, err := s.db.Begin()
    if err != nil {
        return err
    }
    // Files first (FK constraint).
    if _, err := tx.Exec(
        `DELETE FROM files WHERE snapshot_id IN
         (SELECT id FROM snapshots WHERE branch_id = ?)`, branchID,
    ); err != nil {
        tx.Rollback()
        return err
    }
    if _, err := tx.Exec(
        `DELETE FROM snapshots WHERE branch_id = ?`, branchID,
    ); err != nil {
        tx.Rollback()
        return err
    }
    return tx.Commit()
}
```

Call this in `branch.Delete()` between `store.DeleteBranch(b.ID)` and `RemoveWorkspace(...)`.  
Add `--keep-history` flag to `avc branch delete` for users who want to retain orphaned snapshots deliberately.

---

### 2.2 · Object Store Garbage Collection (`avc gc`)

**Priority:** P0 · **Effort:** M  
**Files:** New `avc/internal/gc/gc.go`, new `avc/cmd/avc/gc.go`

#### Problem

`DeleteSnapshot` removes DB rows but never touches the object store. Every deleted snapshot leaves its blobs in `.avc/objects/`. There is no `avc gc` command. A 100-snapshot project with 1 MB of unique changes per snapshot carries ~100 MB of orphaned blobs after pruning.

#### Implementation

**Step 1 — Add `LiveHashes()` to `db.go`:**

```go
// LiveHashes returns the set of all file hashes currently referenced by
// any snapshot in the database. Used by GC to identify safe-to-delete objects.
func (s *Store) LiveHashes() (map[string]bool, error) {
    rows, err := s.db.Query(`SELECT DISTINCT file_hash FROM files`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    live := make(map[string]bool)
    for rows.Next() {
        var h string
        if err := rows.Scan(&h); err != nil {
            return nil, err
        }
        live[h] = true
    }
    return live, rows.Err()
}
```

**Step 2 — Create `avc/internal/gc/gc.go`:**

```go
package gc

import (
    "io/fs"
    "os"
    "path/filepath"

    "github.com/trevarix/agentic-vc/avc/internal/db"
)

// Result summarises a GC run.
type Result struct {
    ScannedObjects int
    DeletedObjects int
    BytesReclaimed int64
    DryRun         bool
}

// Run scans the object store and removes blobs not referenced by any snapshot.
// If dryRun is true, objects are counted and reported but not deleted.
func Run(projectRoot string, dryRun bool) (*Result, error) {
    store, err := db.Open(projectRoot)
    if err != nil {
        return nil, err
    }
    live, err := store.LiveHashes()
    store.Close()
    if err != nil {
        return nil, err
    }

    objectsDir := filepath.Join(projectRoot, ".avc", "objects")
    result := &Result{DryRun: dryRun}

    err = filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, err error) error {
        if err != nil || d.IsDir() {
            return err
        }
        // Reconstruct hash from <shard>/<rest> directory structure.
        shard := filepath.Base(filepath.Dir(path))
        name  := filepath.Base(path)
        hash  := shard + name

        result.ScannedObjects++
        if !live[hash] {
            info, statErr := d.Info()
            if statErr == nil {
                result.BytesReclaimed += info.Size()
            }
            result.DeletedObjects++
            if !dryRun {
                return os.Remove(path)
            }
        }
        return nil
    })
    return result, err
}
```

**Step 3 — CLI command `avc/cmd/avc/gc.go`:**

```
avc gc              # dry run — shows what would be removed (safe default)
avc gc --run        # delete orphaned objects
avc gc --json       # machine-readable: {scanned, deleted, bytes_reclaimed, dry_run}
```

---

### 2.3 · `avc storage` — Disk Usage Accounting

**Priority:** P3 · **Effort:** S  
**Files:** New `avc/cmd/avc/storage.go`

#### Problem

No way to see how much space AVC consumes, or which branch/snapshot is the largest consumer.

#### Implementation

```bash
avc storage                     # total summary
avc storage --by-branch         # per-branch breakdown
avc storage --by-snapshot --limit 10   # top 10 largest snapshots
avc storage --json
```

Example output:
```
AVC storage for: my-project
  Database:        2.4 MB
  Objects:        48.2 MB
  Workspaces:     12.1 MB  (3 active branches)
  Total:          62.7 MB

Top branches by workspace size:
  feature/auth-rewrite    8.3 MB
  fix/payment-bug         2.1 MB

Run `avc gc` to reclaim storage from deleted snapshots.
```

The command reads the DB for snapshot sizes (already stored in `total_size`) and uses `filepath.WalkDir` for object store and workspace sizes.

---

### 2.4 · Snapshot Retention Policy

**Priority:** P2 · **Effort:** M  
**Files:** `avc/internal/config/config.go`, `avc/internal/snapshot/snapshot.go`, new `avc/internal/retention/retention.go`

#### Problem

Snapshots accumulate indefinitely. Users who don't manually prune will eventually hit disk pressure. There is no automatic management.

#### Config additions

```toml
[retention]
# Maximum snapshots to keep per branch (oldest pruned first). 0 = unlimited.
max_snapshots_per_branch = 100

# Delete snapshots older than N days. 0 = unlimited.
max_age_days = 90

# Run gc automatically after pruning. Default: true.
auto_gc = true
```

#### Implementation

```go
// package retention applies the configured retention policy.

// Enforce deletes snapshots that violate the policy for the active branch.
// Called at the end of snapshot.Create() — runs in the background so it
// does not block the snapshot call.
func Enforce(projectRoot string, branchID string, cfg *config.RetentionConfig) error {
    if cfg.MaxSnapshotsPerBranch == 0 && cfg.MaxAgeDays == 0 {
        return nil
    }
    store, err := db.Open(projectRoot)
    if err != nil {
        return err
    }
    defer store.Close()

    snaps, err := store.ListSnapshotsByBranch(branchID)
    if err != nil {
        return err
    }

    var toDelete []string

    // Rule 1: max count (snaps are newest-first).
    if cfg.MaxSnapshotsPerBranch > 0 && len(snaps) > cfg.MaxSnapshotsPerBranch {
        for _, s := range snaps[cfg.MaxSnapshotsPerBranch:] {
            toDelete = append(toDelete, s.ID)
        }
    }

    // Rule 2: max age.
    if cfg.MaxAgeDays > 0 {
        cutoff := time.Now().AddDate(0, 0, -cfg.MaxAgeDays).Unix()
        for _, s := range snaps {
            if s.Timestamp < cutoff {
                toDelete = append(toDelete, s.ID)
            }
        }
    }

    for _, id := range uniqueIDs(toDelete) {
        _ = store.DeleteSnapshot(id) // best-effort
    }

    if cfg.AutoGC && len(toDelete) > 0 {
        _, _ = gc.Run(projectRoot, false)
    }
    return nil
}
```

Print a note to stderr when snapshots are pruned:  
`[avc] Pruned 3 old snapshots on branch 'main' (retention policy: max 100 snapshots)`

---

### Phase 2 — Exit Criteria

- [ ] `avc branch delete <name>` leaves no orphaned snapshot rows in the DB
- [ ] `avc gc` (dry run) correctly identifies unreferenced objects
- [ ] `avc gc --run` deletes exactly those objects and reports bytes reclaimed
- [ ] `avc storage` shows accurate totals matching the filesystem
- [ ] Retention policy prunes oldest snapshots when `max_snapshots_per_branch` is exceeded
- [ ] `go test ./...` passes, including new GC and retention tests

---

## Phase 3 — Agent UX: New MCP Tools

**Goal:** Expose the three most agent-useful operations that exist in the CLI but have no MCP surface, and fix the one MCP tool that is dangerously slow at scale.

**Why now:** Agents are the primary users of the MCP interface. The gaps in Phase 3 are blocking capabilities — "what changed?", "undo just this file", "who wrote this line?" — that agents need constantly.

**Estimated duration:** ~1 week

---

### 3.1 · `avc status` CLI Command + `avc_status` MCP Tool

**Priority:** P1 · **Effort:** S  
**Files:** New `avc/cmd/avc/status.go`, `avc/internal/mcp/tools.go`, `avc/internal/mcp/handlers.go`

#### Problem

There is no equivalent of `git status`. Agents must run a full `avc diff` to check what changed. `diff.CompareWithCurrent` already exists and powers the web server — it just isn't wired to the CLI or MCP.

#### CLI implementation

```go
// avc/cmd/avc/status.go
var statusCmd = &cobra.Command{
    Use:   "status",
    Short: "Show files changed since the last snapshot",
    RunE: func(cmd *cobra.Command, args []string) error {
        root, err := requireInitializedProject()
        if err != nil {
            return err
        }

        store, err := db.Open(root)
        if err != nil {
            return err
        }
        branchID, _ := branch.GetActiveBranchID(root)
        head, headErr := store.GetHeadSnapshot(branchID)
        store.Close()

        if headErr != nil {
            fmt.Fprintln(os.Stderr, "No snapshots yet. Run `avc snapshot` to start tracking.")
            return nil
        }

        result, err := diff.CompareWithCurrent(root, head.ID)
        if err != nil {
            return err
        }

        if jsonFlag {
            return printJSON(result)
        }

        // Human-readable output.
        if len(result.Files) == 0 {
            fmt.Printf("Nothing changed since snapshot %s (%q)\n", head.ID, head.Label)
            return nil
        }
        fmt.Printf("Branch: %s  ·  Last snapshot: %s %q\n\n",
            branch.GetActiveBranchName(root), head.ID, head.Label)
        for _, f := range result.Files {
            symbol := map[string]string{"added": "A", "modified": "M", "deleted": "D"}[string(f.Type)]
            fmt.Printf("  %s  %-40s  +%d -%d\n", symbol, f.Path, f.LinesAdded, f.LinesRemoved)
        }
        fmt.Printf("\n%d file(s) changed. Run `avc snapshot \"<label>\"` to save.\n", len(result.Files))
        return nil
    },
}
```

#### MCP tool definition

```go
{
    Name: "avc_status",
    Description: "Show files changed since the last snapshot on the active branch. " +
        "Use this before avc_snapshot to confirm which files will be captured. " +
        "Returns an empty list if the working tree matches the last snapshot exactly. " +
        "On an agent branch this compares the workspace against its last snapshot.",
    InputSchema: InputSchema{Type: "object"},
},
```

Handler: call `diff.CompareWithCurrent(projectRoot, headSnap.ID)` and return the file list.

---

### 3.2 · `avc_restore_file` MCP Tool

**Priority:** P1 · **Effort:** S  
**Files:** `avc/internal/mcp/tools.go`, `avc/internal/mcp/handlers.go`

#### Problem

`restore.RestoreFile` is fully implemented and exposed in the web server and CLI. Agents have no MCP access. Full snapshot restore is a blunt instrument — agents frequently need to undo one file's changes while keeping all other files intact.

#### Tool definition

```go
{
    Name: "avc_restore_file",
    Description: "Restore a single file from a snapshot without affecting other files. " +
        "On an agent branch this writes to the workspace only — " +
        "the real project root is untouched. " +
        "Use this instead of avc_restore when you only need to undo one file.",
    InputSchema: InputSchema{
        Type: "object",
        Properties: map[string]Property{
            "snapshot_id": {Type: "string", Description: "Snapshot to restore the file from"},
            "path":        {Type: "string", Description: "Relative file path (e.g. 'src/auth.go')"},
        },
        Required: []string{"snapshot_id", "path"},
    },
},
```

#### Handler

```go
func toolRestoreFile(projectRoot string, args map[string]any) (any, error) {
    snapID := strArg(args, "snapshot_id")
    path   := strArg(args, "path")
    if snapID == "" || path == "" {
        return nil, fmt.Errorf("snapshot_id and path are required")
    }

    // Target workspace on non-main branches.
    targetDir := projectRoot
    branchName := branchpkg.GetActiveBranchName(projectRoot)
    if ws := branchpkg.WorkspacePath(projectRoot, branchName); ws != "" {
        targetDir = ws
    }

    result, err := restore.RestoreFileToDir(projectRoot, snapID, path, targetDir)
    if err != nil {
        return nil, err
    }
    return map[string]any{
        "snapshot_id": result.SnapshotID,
        "file_path":   result.FilePath,
        "size":        result.Size,
        "target_dir":  targetDir,
        "success":     true,
    }, nil
}
```

Add `restore.RestoreFileToDir(projectRoot, snapID, path, targetDir)` to `internal/restore/restore_file.go` — a thin wrapper around the existing `RestoreFile` that redirects the output path.

---

### 3.3 · `avc_annotate` MCP Tool

**Priority:** P1 · **Effort:** S  
**Files:** `avc/internal/mcp/tools.go`, `avc/internal/mcp/handlers.go`

#### Problem

`internal/annotate` is fully implemented and the CLI command `avc annotate` works. Agents have no MCP access. Line-level blame is critical for tracing when a specific regression was introduced: "Which snapshot added line 42 of auth.go?"

#### Tool definition

```go
{
    Name: "avc_annotate",
    Description: "Show which snapshot introduced each line of a file. " +
        "Returns [{line, snapshot_id, label, agent_name, timestamp}]. " +
        "Useful for tracing when a regression was introduced. " +
        "Lines that exist on disk but were never snapshotted show snapshot_id: ''.",
    InputSchema: InputSchema{
        Type: "object",
        Properties: map[string]Property{
            "path": {Type: "string", Description: "Relative file path (e.g. 'src/auth.go')"},
        },
        Required: []string{"path"},
    },
},
```

Handler calls `annotate.Annotate(projectRoot, path)` and returns the result directly — `AnnotateResult` is already JSON-serialisable.

---

### 3.4 · Batch `Annotate` Queries (O(N²) → O(1))

**Priority:** P1 · **Effort:** S  
**Files:** `avc/internal/annotate/annotate.go:62`, `avc/internal/db/db.go`

#### Problem

`Annotate` fires one `GetSnapshotFiles` query per snapshot in the project's full history. For a file with 100 snapshots, that is 100 separate DB round trips, each doing a table scan (fixed by Phase 1 indexes, but still 100 queries instead of 1).

#### Implementation

**Step 1 — Add `GetFileVersions` to `db.go`:**

```go
// FileVersion is a single (snapshot, hash, timestamp) tuple for one file version.
type FileVersion struct {
    SnapshotID string
    FileHash   string
    Timestamp  int64
}

// GetFileVersions returns all versions of filePath across all snapshots,
// ordered oldest-first. This is the efficient path for annotate.
func (s *Store) GetFileVersions(filePath string) ([]FileVersion, error) {
    rows, err := s.db.Query(
        `SELECT f.snapshot_id, f.file_hash, s.timestamp
         FROM files f
         JOIN snapshots s ON f.snapshot_id = s.id
         WHERE f.relative_path = ?
         ORDER BY s.timestamp ASC`,
        filePath,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var versions []FileVersion
    for rows.Next() {
        var v FileVersion
        if err := rows.Scan(&v.SnapshotID, &v.FileHash, &v.Timestamp); err != nil {
            return nil, err
        }
        versions = append(versions, v)
    }
    return versions, rows.Err()
}
```

**Step 2 — Rewrite `annotate.Annotate`** to call `store.GetFileVersions(filePath)` (one query) then join against the in-memory snapshot list for label/agent metadata. Remove the N-query inner loop.

---

### 3.5 · `avc_run_in_workspace` Mechanical Approval Enforcement

**Priority:** Architecture (A4) · **Effort:** S  
**Files:** `avc/internal/config/config.go`, `avc/internal/mcp/handlers.go`

#### Problem

The `avc_run_in_workspace` tool description says _"always get explicit user approval before calling"_ — but this is enforced only by the LLM's instruction-following. Under adversarial input or a non-compliant agent, it can be called autonomously.

#### Implementation

Add to `config.toml`:

```toml
[run]
# Set to true to allow avc_run_in_workspace. Default: false.
# Must be explicitly enabled — agents cannot enable this themselves.
enabled = false
```

Add to `config.go`:

```go
type RunConfig struct {
    Enabled               bool `toml:"enabled"`
    DefaultTimeoutSeconds int  `toml:"default_timeout_seconds"`
    MaxTimeoutSeconds     int  `toml:"max_timeout_seconds"`
    MaxOutputKB           int  `toml:"max_output_kb"`
}
```

Add guard at the top of `toolRunInWorkspace`:

```go
func toolRunInWorkspace(projectRoot string, args map[string]any) (any, error) {
    cfg, _ := config.Load(projectRoot)
    if cfg == nil || !cfg.Run.Enabled {
        return nil, fmt.Errorf(
            "avc_run_in_workspace is disabled. " +
            "To enable it, set [run] enabled = true in .avc/config.toml. " +
            "This must be done manually by a human — agents cannot enable it.",
        )
    }
    // ... rest of handler
}
```

Update the tool description to include: _"Requires [run] enabled = true in .avc/config.toml (set by a human, not an agent)."_

---

### Phase 3 — Exit Criteria

- [ ] `avc status` shows correct changed/added/deleted files vs last snapshot
- [ ] `avc status --json` output is valid and documented
- [ ] `avc_status` MCP tool returns the same data
- [ ] `avc_restore_file` restores only the specified file; other files unchanged
- [ ] `avc_restore_file` targets workspace on non-main branches
- [ ] `avc_annotate` returns correct line-origin data for a multi-snapshot file
- [ ] `Annotate` fires exactly 1 DB query (not N) for file version history
- [ ] `avc_run_in_workspace` returns an error when `[run] enabled = false`
- [ ] `go test ./...` passes, including new tests for each tool

---

## Phase 4 — Merge Quality

**Goal:** Make the merge workflow complete — close the post-merge state gap, give agents a structured way to resolve conflicts, and add a compact diff summary mode.

**Estimated duration:** ~3–4 days

---

### 4.1 · Post-Merge Auto-Snapshot of Main

**Priority:** P1 · **Effort:** S  
**Files:** `avc/internal/merge/merge.go:165`

#### Problem

After a successful merge, the new state of main is never snapshotted. The last main snapshot is the `pre-merge:` safety snapshot, not the merged result. `avc list` on main after a merge shows the pre-merge state as HEAD — misleading and wrong as a baseline for the next diff.

#### Implementation

In `merge.go`, after the file application loop succeeds, before updating merge status:

```go
// Auto-snapshot main to capture the merged state as the new HEAD.
postSnap, postErr := snapshot.Create(
    projectRoot,
    fmt.Sprintf("post-merge: merged branch '%s'", branchName),
    "avc-merge",
    fmt.Sprintf("automatic snapshot after clean merge of '%s'", branchName),
    mainBranch.ID,
    "", // main always uses project root
)
if postErr != nil {
    // Non-fatal: merge succeeded; log but don't fail.
    fmt.Fprintf(os.Stderr, "[avc] warning: post-merge snapshot failed: %v\n", postErr)
} else {
    result.PostMergeSnapshotID = postSnap.ID
}
```

Add `PostMergeSnapshotID string` to the `Result` struct and expose it in JSON output.

---

### 4.2 · Conflict Resolution MCP Tools

**Priority:** P3 · **Effort:** M  
**Files:** `avc/internal/mcp/tools.go`, `avc/internal/mcp/handlers.go`, `avc/internal/merge/resolve.go`

#### Problem

When `avc_merge` returns conflicts, agents must manually edit conflict marker text with no structure. There is no tool to say "accept theirs for path X" or "list all unresolved conflicts."

#### New tools

**`avc_list_conflicts(branch)`**  
Returns all files with unresolved conflict markers for the last merge on `branch`. Scans the workspace for `<<<<<<< main` markers.

```go
{
    Name: "avc_list_conflicts",
    Description: "List all files with unresolved merge conflict markers for the last merge. " +
        "Call after avc_merge reports conflicts to see what needs resolution.",
    InputSchema: InputSchema{
        Type: "object",
        Properties: map[string]Property{
            "branch": {Type: "string", Description: "Branch name"},
        },
        Required: []string{"branch"},
    },
},
```

**`avc_resolve_conflict(branch, path, resolution)`**  
Resolves one file by writing the chosen version (ours/theirs) or custom content.

```go
{
    Name: "avc_resolve_conflict",
    Description: "Resolve a conflict in one file. After resolving all conflicts, " +
        "call avc_merge again to complete the merge.",
    InputSchema: InputSchema{
        Type: "object",
        Properties: map[string]Property{
            "branch":     {Type: "string", Description: "Branch name"},
            "path":       {Type: "string", Description: "Relative file path"},
            "resolution": {Type: "string",
                Description: "'ours' (keep main), 'theirs' (keep branch), or 'content' (provide resolved text)"},
            "content":    {Type: "string",
                Description: "Resolved file content — only used when resolution is 'content'"},
        },
        Required: []string{"branch", "path", "resolution"},
    },
},
```

#### Implementation in `merge/resolve.go`

```go
// ResolveFile writes the chosen version of a conflicted file.
func ResolveFile(projectRoot, branchName, filePath, resolution, content string) error {
    dest := filepath.Join(projectRoot, filepath.FromSlash(filePath))

    switch resolution {
    case "ours":
        // Read main hash from the last merge record.
        hash, err := getMainHash(projectRoot, branchName, filePath)
        if err != nil { return err }
        data, err := restore.ReadObject(projectRoot, hash)
        if err != nil { return err }
        return fileutil.WriteFile(dest, data)

    case "theirs":
        hash, err := getBranchHash(projectRoot, branchName, filePath)
        if err != nil { return err }
        data, err := restore.ReadObject(projectRoot, hash)
        if err != nil { return err }
        return fileutil.WriteFile(dest, data)

    case "content":
        if content == "" {
            return fmt.Errorf("content must be provided when resolution is 'content'")
        }
        return fileutil.WriteFile(dest, []byte(content))

    default:
        return fmt.Errorf("resolution must be 'ours', 'theirs', or 'content'")
    }
}
```

---

### 4.3 · `avc diff --stat` Compact Summary Mode

**Priority:** P3 · **Effort:** S  
**Files:** `avc/cmd/avc/diff.go`

#### Problem

`avc diff` always shows full unified diff text — verbose for a quick overview. No compact summary mode.

#### Implementation

Add `--stat` flag to `avc diff`:

```bash
avc diff snap-abc123 snap-def456 --stat
```

Output:
```
 src/auth.go           | +15  -3
 src/users.go          | +42  -8
 tests/auth_test.go    | +67 -12
 ──────────────────────────────────
 3 files changed  +124 -23
```

All data already comes from `diff.Compare()`. This is a display-only change — no new DB queries. Also add `--stat` to `avc branch diff` for the same treatment.

---

### Phase 4 — Exit Criteria

- [ ] `avc list` on main after a clean merge shows the post-merge snapshot as HEAD
- [ ] `result.post_merge_snapshot_id` present in `avc_merge` JSON output
- [ ] `avc_list_conflicts` correctly identifies files with `<<<<<<<` markers
- [ ] `avc_resolve_conflict` with `"ours"` writes main content; `"theirs"` writes branch content
- [ ] `avc_resolve_conflict` with `"content"` writes the provided text verbatim
- [ ] `avc diff --stat` outputs the summary format; no unified diff text
- [ ] `go test ./...` passes

---

## Phase 5 — Snapshot Discovery & Organisation

**Goal:** Make it fast and easy to find the right snapshot in a long history — by searching labels, filtering by agent, querying by file, or tagging important milestones.

**Estimated duration:** ~1 week

---

### 5.1 · Enhanced `avc list` Filters + `avc search`

**Priority:** P2 · **Effort:** S  
**Files:** `avc/cmd/avc/list.go`, `avc/internal/db/db.go`

#### Problem

With many snapshots, finding the right one requires scrolling through a long list or memorising IDs. There is no search capability.

#### New filter flags for `avc list`

```bash
avc list --search "auth refactor"       # full-text search on label + notes
avc list --agent claude                 # filter by agent_name
avc list --since 2024-06-01             # snapshots after this date
avc list --until 2024-06-30             # snapshots before this date
avc list --changed src/auth.go          # snapshots that included this file
avc list --limit 20                     # default 50, 0 = unlimited
```

**Add `ListSnapshotsFiltered` to `db.go`:**

```go
type SnapshotFilter struct {
    BranchID  string
    AgentName string  // LIKE match
    Query     string  // search label + notes
    Since     int64   // Unix timestamp
    Until     int64   // Unix timestamp
    FilePath  string  // snapshots containing this file
    Limit     int
}

func (s *Store) ListSnapshotsFiltered(f SnapshotFilter) ([]*Snapshot, error) {
    // Build WHERE clause dynamically.
    // For FilePath: use EXISTS (SELECT 1 FROM files WHERE snapshot_id = s.id AND relative_path = ?)
    // For Query:    use (label LIKE ? OR notes LIKE ?)  with '%'+f.Query+'%'
}
```

**Alias `avc search` → `avc list --search`:**

```bash
avc search "before auth refactor"   # shorthand for avc list --search
```

The `--changed <file>` filter is particularly powerful for agents: "find the snapshot that last modified auth.go."

---

### 5.2 · Snapshot Tags

**Priority:** P2 · **Effort:** M  
**Files:** `avc/internal/db/db.go`, new `avc/cmd/avc/tag.go`, `avc/internal/mcp/tools.go`

#### Problem

No machine-readable way to mark a snapshot as "stable", "v1.0.0", or "pre-release". The `notes` field is free text and not filterable. Agents cannot programmatically identify "the last known-good snapshot."

#### Schema addition (migration, idempotent)

```sql
CREATE TABLE IF NOT EXISTS snapshot_tags (
    snapshot_id TEXT NOT NULL,
    tag         TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    PRIMARY KEY (snapshot_id, tag),
    FOREIGN KEY (snapshot_id) REFERENCES snapshots(id)
);
CREATE INDEX IF NOT EXISTS idx_snapshot_tags_tag
    ON snapshot_tags(tag);
```

#### CLI

```bash
avc snapshot tag <id> stable        # apply tag
avc snapshot tag <id> v1.2.0        # version tag
avc snapshot untag <id> stable      # remove tag
avc list --tag stable               # filter snapshots by tag
```

#### DB methods to add

```go
func (s *Store) TagSnapshot(snapshotID, tag string) error { ... }
func (s *Store) UntagSnapshot(snapshotID, tag string) error { ... }
func (s *Store) ListSnapshotsByTag(tag string) ([]*Snapshot, error) { ... }
func (s *Store) GetSnapshotTags(snapshotID string) ([]string, error) { ... }
```

#### MCP tools

```
avc_tag_snapshot(snapshot_id, tag)      # apply a tag
avc_untag_snapshot(snapshot_id, tag)    # remove a tag
```

Extend `avc_list` to accept an optional `tag` parameter.

---

### 5.3 · Diff Cache Invalidation

**Priority:** P2 · **Effort:** S  
**Files:** `avc/internal/db/db.go:554`, `avc/cmd/avc/diff.go`

#### Problem

The `diffs` table cache has no TTL, no invalidation, and no `--no-cache` flag. Stale entries would persist forever if an object were somehow corrupted.

#### Implementation

Add `computed_at` to the `diffs` table (migration):

```sql
ALTER TABLE diffs ADD COLUMN computed_at INTEGER NOT NULL DEFAULT 0;
```

Add `--no-cache` flag to `avc diff`:

```go
diffCmd.Flags().BoolVar(&noCacheFlag, "no-cache", false,
    "recompute diff, ignoring the cache")
```

Add `avc cache` subcommand:

```bash
avc cache clear         # truncate the diffs table
avc cache stats         # show row count and oldest entry date
```

---

### Phase 5 — Exit Criteria

- [ ] `avc list --search "foo"` returns only snapshots whose label or notes contain "foo"
- [ ] `avc list --changed src/auth.go` returns only snapshots that tracked that file
- [ ] `avc list --agent claude` filters correctly
- [ ] `avc search` is a working alias for `avc list --search`
- [ ] `avc snapshot tag <id> stable` adds the tag; `avc list --tag stable` retrieves it
- [ ] `avc snapshot untag` removes the tag
- [ ] `avc diff --no-cache` bypasses the diff cache
- [ ] `avc cache clear` truncates the diffs table
- [ ] `go test ./...` passes

---

## Phase 6 — Web UI Completeness

**Goal:** Bring the standalone web UI (`avc run`) to feature parity with the CLI across all four primitives — snapshot, diff, branch, and merge.

**Estimated duration:** ~1 week

---

### 6.1 · Shared `internal/api/` Package

**Priority:** Architecture (A1) · **Effort:** M  
**Files:** New `avc/internal/api/`, refactor `avc/internal/web/server.go`, `avc/cmd/avc/`

#### Problem

Operation logic is duplicated between CLI command files and web server handlers. Every new feature (branch, merge, etc.) currently requires two separate implementations. The web server missed all Phase 4–5 features because of this.

#### Implementation

Extract a shared package `avc/internal/api/` with typed operation functions:

```go
package api

// SnapshotOps groups all snapshot operations.
type SnapshotOps struct{ ProjectRoot string }
func (o SnapshotOps) List(f db.SnapshotFilter) ([]*db.Snapshot, error) { ... }
func (o SnapshotOps) Create(label, agent, notes, branchID, sourceDir string) (*snapshot.Result, error) { ... }
func (o SnapshotOps) Info(id string) (*db.Snapshot, []*db.File, error) { ... }
func (o SnapshotOps) Delete(id string) error { ... }
func (o SnapshotOps) Tag(id, tag string) error { ... }

// BranchOps groups all branch operations.
type BranchOps struct{ ProjectRoot string }
func (o BranchOps) List() ([]*db.Branch, error) { ... }
func (o BranchOps) Create(name, fromSnapshotID string) (*db.Branch, error) { ... }
func (o BranchOps) Switch(name string) error { ... }
func (o BranchOps) Delete(name string) error { ... }
func (o BranchOps) Rename(oldName, newName string) error { ... }
func (o BranchOps) Diff(name string) (*diff.Result, error) { ... }

// MergeOps groups all merge operations.
type MergeOps struct{ ProjectRoot string }
func (o MergeOps) Preview(branchName string) (*merge.Result, error) { ... }
func (o MergeOps) Merge(branchName string) (*merge.Result, error) { ... }
func (o MergeOps) Abort() error { ... }
```

Both `cmd/avc/` and `internal/web/server.go` call `api.*` — no logic in either. Future features are implemented once in `api/`, and both surfaces get them automatically.

---

### 6.2 · Web Server Branch and Merge Endpoints

**Priority:** P2 · **Effort:** M  
**Files:** `avc/internal/web/server.go`

#### New endpoints

| Method | Path | Action |
|--------|------|--------|
| `GET` | `/api/branches` | List all branches with workspace paths |
| `POST` | `/api/branches` | Create branch `{"name": "..."}` |
| `POST` | `/api/branches/switch` | Switch active branch `{"name": "..."}` |
| `DELETE` | `/api/branches/:name` | Delete branch |
| `GET` | `/api/branches/:name/diff` | Branch cumulative diff |
| `GET` | `/api/merge/preview?branch=x` | Merge preview (dry run) |
| `POST` | `/api/merge` | Execute merge `{"branch": "..."}` |
| `POST` | `/api/merge/abort` | Abort in-progress merge |
| `GET` | `/api/status` | Working tree status (Phase 3.1) |
| `GET` | `/api/storage` | Storage accounting (Phase 2.3) |

Each handler follows the existing pattern: validate method → call `api.*` → `writeJSON`. No new logic.

---

### 6.3 · Web UI Front-End for Branches and Merges

**Priority:** P2 · **Effort:** M  
**Files:** `avc/internal/web/static/`

Add to the web front-end:

- **Branch selector dropdown** in the header (fetches `/api/branches`, switches via `POST /api/branches/switch`)
- **Branch diff view** — link per branch to show cumulative diff
- **Merge panel** — "Merge to main" button that calls preview, then shows clean/conflict counts, then confirms
- **Status panel** — shows current working-tree changes (Phase 3.1)

---

### Phase 6 — Exit Criteria

- [ ] `internal/api/` package exists and all CLI/web handlers call it (no duplicated logic)
- [ ] `GET /api/branches` returns correct branch list
- [ ] `POST /api/branches` creates a branch and workspace
- [ ] `DELETE /api/branches/:name` deletes branch and workspace
- [ ] `GET /api/branches/:name/diff` returns cumulative diff JSON
- [ ] `POST /api/merge` returns correct clean/conflict result
- [ ] `POST /api/merge/abort` restores pre-merge state
- [ ] Web UI branch selector changes the active branch and refreshes snapshot list
- [ ] `go test ./...` passes

---

## Phase 7 — Branch Lifecycle & Automation

**Goal:** Give branches explicit lifecycle states, let users rename branches, and add pre/post hooks for automation — rounding out the branch primitive.

**Estimated duration:** ~1 week

---

### 7.1 · Branch Lifecycle States

**Priority:** P2 · **Effort:** M  
**Files:** `avc/internal/db/db.go`, `avc/internal/branch/branch.go`

#### Problem

Branches are either "exists" or "deleted." After a merge, branches linger with workspaces consuming disk and no indication they've been integrated. There's no way to mark a branch as abandoned without deleting its history.

#### Schema change

```sql
ALTER TABLE branches ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
-- Values: 'active' | 'merged' | 'abandoned'
```

#### Behaviour changes

- After a successful `avc merge`, automatically set `status = 'merged'` (do not auto-delete — preserve history)
- `avc branch list` shows a `Status` column; filters to `active` by default
- `avc branch list --all` shows all statuses
- `avc branch list --status merged` shows merged branches
- `avc branch prune --merged` deletes workspaces for all `merged` branches (keeps DB records and snapshots)
- `avc branch abandon <name>` sets `status = 'abandoned'` without removing anything

#### MCP additions

```
avc_branch_abandon(name)        # mark as abandoned
avc_branch_prune_merged()       # remove workspaces for all merged branches
```

---

### 7.2 · `avc branch rename`

**Priority:** P3 · **Effort:** S  
**Files:** `avc/internal/branch/branch.go`, `avc/internal/mcp/tools.go`

#### Problem

Branches cannot be renamed after creation. The only option is delete (losing history) + recreate.

#### Implementation

```go
// Rename renames a branch: updates the DB record, renames the workspace
// directory, and updates config.toml if it was the active branch.
func Rename(projectRoot, oldName, newName string) error {
    if err := ValidateBranchName(newName); err != nil {
        return err
    }

    store, err := db.Open(projectRoot)
    if err != nil { return err }
    defer store.Close()

    proj, err := store.GetProject(projectRoot)
    if err != nil { return err }

    b, err := store.GetBranchByName(proj.ID, oldName)
    if err != nil { return fmt.Errorf("branch '%s' not found", oldName) }

    // Check new name doesn't already exist.
    if _, err := store.GetBranchByName(proj.ID, newName); err == nil {
        return fmt.Errorf("branch '%s' already exists", newName)
    }

    // Rename workspace first (reversible if DB fails).
    oldWS := WorkspacePath(projectRoot, oldName)
    newWS := WorkspacePath(projectRoot, newName)
    if _, statErr := os.Stat(oldWS); statErr == nil {
        if err := os.Rename(oldWS, newWS); err != nil {
            return fmt.Errorf("rename workspace: %w", err)
        }
    }

    // Update DB.
    if err := store.RenameBranch(b.ID, newName); err != nil {
        os.Rename(newWS, oldWS) // rollback workspace rename
        return err
    }

    // Update active branch if needed.
    if GetActiveBranchName(projectRoot) == oldName {
        return config.SetActiveBranch(projectRoot, newName)
    }
    return nil
}
```

Add `store.RenameBranch(id, newName)` to `db.go`: `UPDATE branches SET name = ? WHERE id = ?`.

CLI: `avc branch rename <old> <new>`  
MCP: `avc_branch_rename(old_name, new_name)`

---

### 7.3 · Active Branch in SQLite (Eliminate Config.toml Race)

**Priority:** Architecture (A2) · **Effort:** M  
**Files:** `avc/internal/db/db.go`, `avc/internal/config/config.go`, `avc/internal/branch/branch.go`

#### Problem

Active branch is stored in `.avc/config.toml` outside the DB's transactional boundary. Even with the file lock from Phase 1.3, this is architectural friction: the state is managed separately from the rest of the branch metadata, and reads require parsing TOML instead of a SQLite query.

#### Implementation

Add to schema:

```sql
CREATE TABLE IF NOT EXISTS project_state (
    project_id    TEXT PRIMARY KEY,
    active_branch TEXT NOT NULL DEFAULT 'main',
    updated_at    INTEGER NOT NULL,
    FOREIGN KEY (project_id) REFERENCES projects(id)
);
```

Add DB methods:

```go
func (s *Store) GetActiveBranch(projectID string) (string, error) { ... }
func (s *Store) SetActiveBranch(projectID, name string) error {
    _, err := s.db.Exec(
        `INSERT INTO project_state (project_id, active_branch, updated_at)
         VALUES (?, ?, ?)
         ON CONFLICT(project_id) DO UPDATE SET
             active_branch = excluded.active_branch,
             updated_at    = excluded.updated_at`,
        projectID, name, time.Now().Unix(),
    )
    return err
}
```

Update `branch.GetActiveBranchName` and `config.SetActiveBranch` to use the DB method. Keep `config.toml` `[branch] active` as a fallback read path for backwards compatibility during the transition period, but write only to the DB going forward. Remove the file lock from Phase 1.3 once all reads/writes go through SQLite.

---

### 7.4 · Pre/Post Snapshot Hooks

**Priority:** P3 · **Effort:** M  
**Files:** `avc/internal/config/config.go`, `avc/internal/snapshot/snapshot.go`, new `avc/internal/hooks/hooks.go`

#### Problem

No way to enforce "only snapshot if tests pass" or to trigger external notifications after a snapshot.

#### Config additions

```toml
[hooks]
# Run before creating a snapshot. Non-zero exit aborts the snapshot.
pre_snapshot  = "npm test -- --silent"

# Run after a successful snapshot. $AVC_SNAPSHOT_ID is set.
post_snapshot = ""

# Run before a restore.
pre_restore   = ""

# Run after a successful restore.
post_restore  = ""
```

#### Implementation in `hooks/hooks.go`

```go
package hooks

// Run executes the configured hook command with AVC environment variables.
// Returns an error (which aborts the calling operation) only for pre-hooks.
// Post-hook errors are logged to stderr but do not fail the operation.
func Run(projectRoot, command, snapshotID, branchName string) error {
    if command == "" {
        return nil
    }
    env := []string{
        "AVC_PROJECT_ROOT=" + projectRoot,
        "AVC_SNAPSHOT_ID=" + snapshotID,
        "AVC_BRANCH=" + branchName,
    }
    // Run with workspace sandbox on non-main branches (same as avc_run_in_workspace),
    // project root on main.
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()
    var cmd *exec.Cmd
    if runtime.GOOS == "windows" {
        cmd = exec.CommandContext(ctx, "cmd", "/C", command)
    } else {
        cmd = exec.CommandContext(ctx, "sh", "-c", command)
    }
    cmd.Dir = projectRoot
    cmd.Env = append(os.Environ(), env...)
    return cmd.Run()
}
```

---

### Phase 7 — Exit Criteria

- [ ] `avc merge <branch>` sets branch status to `'merged'`
- [ ] `avc branch list` shows only `active` branches by default
- [ ] `avc branch list --all` shows all branches with status column
- [ ] `avc branch prune --merged` removes workspaces for merged branches
- [ ] `avc branch rename <old> <new>` renames branch, workspace, and config reference
- [ ] Active branch reads/writes use the `project_state` DB table
- [ ] `pre_snapshot` hook that exits non-zero aborts the snapshot
- [ ] `post_snapshot` hook runs after successful snapshot with `AVC_SNAPSHOT_ID` set
- [ ] `go test ./...` passes

---

## Phase 8 — Portability & Performance

**Goal:** Make AVC portable across machines and make it fast on large projects. This is the final pre-Marketplace phase.

**Estimated duration:** ~2 weeks

---

### 8.1 · `avc export` / `avc import`

**Priority:** P2 · **Effort:** L  
**Files:** New `avc/internal/archive/`, new `avc/cmd/avc/export.go`, new `avc/cmd/avc/import.go`

#### Problem

AVC history is local-only with no backup or portability mechanism. Users cannot move history to another machine, share snapshot history with a collaborator, or persist agent snapshots across ephemeral CI runners.

#### Implementation

```bash
avc export --output my-project.avc.tar.gz          # full export
avc export --branch feature/auth --output auth.tar.gz  # branch-only export
avc import --from my-project.avc.tar.gz            # merge into current project
avc import --from auth.tar.gz --as feature/auth-imported
```

**Export bundle structure:**

```
my-project.avc.tar.gz
├── avc-export.json       # {version, project_name, exported_at, branches[]}
├── schema.sql            # SQLite .dump output (portable, human-readable)
└── objects/
    ├── ab/cdef...        # all referenced blobs
    └── ff/0011...
```

**Export algorithm:**

1. Run `SELECT ... INTO OUTFILE` / SQLite `.dump` to get a portable schema dump
2. Collect all `file_hash` values from exported snapshots
3. Copy exactly those objects into the archive (dedup across snapshots is automatic)
4. Write `avc-export.json` manifest with version and metadata

**Import algorithm:**

1. Verify `avc-export.json` version compatibility
2. Replay `schema.sql` into a temporary DB
3. Merge branches, snapshots, and files into the current DB (re-generate IDs on conflict)
4. Copy objects into `.avc/objects/` (no-op if already present — content-addressed)
5. Report: `Imported 47 snapshots, 3 branches, 1.2 GB objects`

---

### 8.2 · Workspace Hardlink / Reflink Optimisation

**Priority:** P3 · **Effort:** M  
**Files:** `avc/internal/branch/branch.go:55` (`copyToWorkspace`)

#### Problem

`copyToWorkspace` reads and writes every tracked file byte-for-byte. On a 200 MB project, every branch creation copies 200 MB. On APFS (macOS), Btrfs, and ReFS (Windows Server 2016+), copy-on-write (reflink) clones take microseconds and use zero extra disk space until individual files diverge.

#### Implementation

```go
// copyToWorkspaceOptimized attempts reflink/hardlink on supported filesystems.
// Falls back transparently to regular copy on EXDEV or EOPNOTSUPP.
func copyToWorkspaceOptimized(src, dst string) error {
    if err := tryReflink(src, dst); err == nil {
        return nil
    }
    if err := tryHardlink(src, dst); err == nil {
        return nil
    }
    return regularCopy(src, dst)
}
```

Platform-specific implementations:
- **macOS** (`copyopt_darwin.go`): `clonefile(2)` via `golang.org/x/sys/unix`
- **Linux** (`copyopt_linux.go`): `ioctl(fd, FICLONE, ...)` 
- **Windows** (`copyopt_windows.go`): `FSCTL_DUPLICATE_EXTENTS_TO_FILE`
- **Fallback** (`copyopt_other.go`): current `io.Copy` path

No behaviour change for users — workspace creation is simply much faster on supported filesystems.

---

### 8.3 · MCP Tool Tiers

**Priority:** Architecture (A3) · **Effort:** S  
**Files:** `avc/internal/mcp/tools.go`, `avc/cmd/avc/mcp.go`

#### Problem

The tool set grows to ~20 tools with all Phase 3–7 additions. Agents with smaller context windows see all tool descriptions consuming a significant portion of their context budget, even when they only need a subset.

#### Implementation

Add `--tools` flag to `avc mcp serve`:

```bash
avc mcp serve --tools core        # 4 tools: snapshot, list, diff, restore
avc mcp serve --tools standard    # 10 tools: + branch, merge (default)
avc mcp serve --tools full        # all ~20 tools
```

```go
func ToolsForTier(tier string) []Tool {
    switch tier {
    case "core":
        return coreTools()     // snapshot, list, diff, restore
    case "full":
        return AllTools()
    default: // "standard"
        return standardTools() // core + branch_create/list/switch/diff + merge + merge_abort
    }
}
```

Update `avc init --skills <framework>` to write the tier in the MCP config it generates (default: `standard`; annotated with a comment explaining how to change it).

---

### 8.4 · Cross-Platform Binaries + VSCode Marketplace Release

**Priority:** Release · **Effort:** M  
**Files:** Build scripts, `extension/package.json`

#### Deliverables

- [ ] `goreleaser` config producing: `avc` (Linux amd64/arm64), `avc.exe` (Windows amd64), `avc-darwin-amd64`, `avc-darwin-arm64`
- [ ] GitHub Actions release workflow: tag → build → attach binaries to release
- [ ] `npm run package` → `.vsix` using `vsce`
- [ ] VSCode Marketplace listing (publisher account, icons, README gallery images)
- [ ] `docs/cli-reference.md` updated with all Phase 3–7 commands (branch rename, status, gc, storage, search, tag, export/import, run tiers)
- [ ] `docs/architecture.md` updated with Phase 6 `internal/api/` section and Phase 7 `project_state` table
- [ ] Performance benchmarks pass: snapshot 50 MB < 2s, diff < 500ms, restore < 5s, branch create < 100ms (with reflink where available), merge < 3s

---

### Phase 8 — Exit Criteria

- [ ] `avc export` produces a valid `.tar.gz` that can be opened and inspected
- [ ] `avc import` round-trip: export from project A, import into project B, all snapshots and objects present and restorable
- [ ] Branch creation on APFS is measurably faster than byte-copy (benchmark test)
- [ ] `avc mcp serve --tools core` advertises exactly 4 tools
- [ ] `avc mcp serve --tools standard` advertises exactly 10 tools
- [ ] Cross-platform binaries build cleanly via CI
- [ ] `.vsix` packages successfully and installs in VSCode without errors
- [ ] All documentation updated — no ⬜ items in `docs/implementation-plan.md` for Phase 3–8 commands
- [ ] Full integration test suite passes: `init → branch → snapshot → merge → verify main → gc → export → import`
- [ ] `go test -count=1 ./...` passes on Linux, macOS, and Windows

---

## Summary Table

| Phase | Goal | Key Deliverables | Est. Duration |
|-------|------|-----------------|---------------|
| **1** | Correctness foundation | WAL+indexes, branch validation, config race fix, diff3 markers | 1 week |
| **2** | Storage management | GC, cascade delete, `avc storage`, retention policy | 1 week |
| **3** | Agent MCP UX | `avc_status`, `avc_restore_file`, `avc_annotate`, batch annotate, run approval gate | 1 week |
| **4** | Merge quality | Post-merge snapshot, conflict resolution tools, `--stat` flag | 3–4 days |
| **5** | Discovery & org | `avc list` filters, `avc search`, snapshot tags, diff cache | 1 week |
| **6** | Web UI parity | `internal/api/` package, branch+merge endpoints, front-end updates | 1 week |
| **7** | Branch lifecycle | Branch states, rename, active branch in DB, snapshot hooks | 1 week |
| **8** | Portability & release | Export/import, reflink workspace, MCP tiers, binaries, Marketplace | 2 weeks |

**Total estimated duration:** ~9–10 weeks (one engineer)

---

## Acceptance Criteria (applies to every item in every phase)

1. ✅ All existing `go test ./...` tests still pass
2. ✅ New tests cover the happy path and at least one error path for each item
3. ✅ Every CLI command has `--json` output and `--help` text
4. ✅ Every new MCP tool has an accurate, agent-readable description
5. ✅ `docs/cli-reference.md` updated for any new command or flag
6. ✅ No phase is started until the prior phase's exit criteria are all green

---

*Generated: 2026-05-23 · Based on full codebase review of Phases 1–7*
