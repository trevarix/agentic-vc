# Plan 03 — Lifecycle Hardening

**Covers review items:** 2.3, 2.4, 2.5, 2.9, 2.10 a–i
**Goal:** Retention, GC, diff, and the auxiliary surfaces (web, MCP transport, ignore rules, repo hygiene) behave correctly under scale and concurrency.
**Prerequisite:** Plans 01–02 complete.
**Estimated duration:** ~1–2 weeks

---

## 1 · Retention/delete must not destroy load-bearing snapshots (review 2.3)

**Priority:** P0 · **Effort:** M
**Files:** `avc/internal/retention/retention.go:50-76`, `avc/internal/db/db.go`, `avc/cmd/avc/delete.go`

### Problem

`branches.base_snapshot_id` and the three snapshot references in `merges` carry no FK constraints, and both `retention.Enforce` and `avc delete` prune blindly. Deleting a branch's merge base makes `buildPlan` read an empty base file set → spurious conflicts or wrong clean-merges. Tagged snapshots (`stable`) are also pruned.

### Implementation

**Step 1 — a single "protected snapshots" query** in `db.go`:

```go
// ProtectedSnapshotIDs returns snapshot IDs that must not be deleted:
//   - base_snapshot_id of any branch with status 'active'
//   - any snapshot with at least one tag
//   - base/main/head snapshot of the most recent merge per branch
func (s *Store) ProtectedSnapshotIDs(projectID string) (map[string]bool, error)
```

**Step 2 — retention exempts them.** In `Enforce`, load the set once and skip protected IDs when building `toDelete`; report `skipped_protected` in the stderr note.

**Step 3 — manual delete refuses with an explanation.**

```
$ avc delete snap-abc123
Error: snapshot snap-abc123 is the base of active branch 'feature/x'.
Delete or merge the branch first, or pass --force to delete anyway.
```

`--force` exists because users own their data; retention never force-deletes. Same check in the MCP `avc_delete` handler (no force parameter — agents don't get to override).

**Step 4 — stop swallowing per-row errors.** `_ = store.DeleteSnapshot(id)` in retention becomes collect-and-log (stderr), so genuine failures are visible.

### Tests

Retention with `max_snapshots_per_branch=1` on a branch whose oldest snapshot is a merge base → base survives. Tagged snapshot survives age-based pruning. `avc delete` on a branch base fails without `--force`, succeeds with it.

---

## 2 · GC grace period + stale-merge awareness (review 2.4)

**Priority:** P0 · **Effort:** S
**Files:** `avc/internal/gc/gc.go`

### Problem

Snapshots store objects *before* inserting DB rows; GC computes the live set, closes the DB, then deletes. An object written by a concurrent snapshot inside that window is collected — and `auto_gc = true` makes GC run after every snapshot, so two concurrent agents hit this in normal operation.

### Implementation

Skip objects younger than a grace window (git's approach):

```go
const graceWindow = 15 * time.Minute

info, statErr := d.Info()
if statErr == nil && time.Since(info.ModTime()) < graceWindow {
    result.SkippedRecent++
    return nil // too young — may belong to an in-flight snapshot
}
```

Expose `--grace <duration>` on `avc gc` (default 15m, `0` allowed for tests), report `skipped_recent` in JSON output. Also sweep `*.tmp` files older than the grace window (from Plan 01·3).

*Deliberately deferred:* a project-wide advisory lock for gc/merge/restore is the complete fix but adds cross-platform lock-file machinery; the grace window removes the realistic race at trivial cost. Revisit if Plan 05 fleet features surface real contention.

### Tests

Object with fresh mtime survives GC even when unreferenced; same object with backdated mtime (`os.Chtimes`) is collected.

---

## 3 · Bounded diff + binary detection (review 2.5)

**Priority:** P1 · **Effort:** S
**Files:** `avc/internal/diff/diff.go:126-221`

### Problem

`lcsLength` — called per modified file for line counts — has no size cap (only the preview path is capped at 2,000 lines). Two versions of a 300k-line generated file cost ~10¹¹ comparisons; `avc diff`, `avc status`, and the extension sidebar hang. Binary files are diffed as text.

### Implementation

**Step 1 — binary detection first:**

```go
// isBinary reports whether data looks like binary content (NUL byte in the
// first 8 KB, the same heuristic git uses).
func isBinary(data []byte) bool
```

In `enrichWithLineCounts`: if either side is binary → `fd.Binary = true`, counts 0, preview `""`. Add `binary` to the JSON shape; CLI renders `(binary file)` and the extension/web show a placeholder instead of an empty diff.

**Step 2 — cap the count path with the existing constant:**

```go
if len(oldLines) > maxDiffFileLines || len(newLines) > maxDiffFileLines {
    // Estimate without LCS: an upper bound that is obvious and cheap.
    return len(newLines), len(oldLines), "", /* estimated: */ true
}
```

Surface `counts_estimated: true` in JSON so consumers can label it (`~+3021 -2998`).

**Step 3 —** replace the O(n²) insertion sort in `sortDiffs` with `sort.Slice` (same ordering).

### Tests

NUL-containing file → `binary`, no line counts. 3,000-line file pair → returns instantly with `counts_estimated`. Benchmark guard: diff of two 500k-line files completes < 1 s.

---

## 4 · Branch delete uses the authoritative active branch (review 2.9)

**Priority:** P1 · **Effort:** S
**Files:** `avc/internal/branch/branch.go:236-242`

`Delete` reads the active branch from `config.toml`, but `project_state` (DB) has been authoritative since Phase 7.3. Replace the config read with `GetActiveBranchName(projectRoot)` (which already prefers the DB and falls back to config). One-line change plus a test: set active via DB only, attempt delete, expect refusal.

---

## 5 · Small-items sweep (review 2.10 a–i)

Grouped by file area; each is independent.

### 5a · `.avcignore` semantics (S→M) — `fileutil.go:94-151`

- Implement real `**` support (segment-wise matcher; `filepath.Match` per segment, `**` matches any number of segments). The doc comment currently advertises `**` falsely.
- Add `!` negation (last matching pattern wins, git-style ordering).
- Document the bare-name component match (`node_modules` matches at any depth) in `docs/cli-reference.md` — it's a feature, but an undocumented surprise today.
- Table-driven tests: `**/*.log`, `build/**`, `!build/keep.txt`, bare names, anchored paths.

### 5b · File modes, symlinks, empty dirs (M) — `db.go`, `snapshot.go`, `restore.go`, `fileutil.go`

- Add `file_mode INTEGER` to `files` (idempotent `ALTER TABLE`, default 0 = legacy → restore as 0644).
- Snapshot records `info.Mode() & (fs.ModePerm | fs.ModeSymlink)`; restore applies perm bits via `os.Chmod` (no-op effect on Windows, correct on Unix — exec bits survive round-trips).
- Symlinks: store the link target as the blob content with the symlink mode bit; restore recreates the link on Unix, writes the target path as a file on Windows (documented limitation). Walk must use `d.Type()` (already non-following) + `os.Lstat` instead of `os.Stat`.
- Restore removes directories left empty by the deletion sweep (post-pass, depth-first).

### 5c · Large-file memory (S, partial) — `fileutil.go`, `snapshot.go`

Full streaming is invasive; do the cheap 90%: files larger than a threshold (default 100 MB, `[snapshot] max_file_size_mb` in config) are **skipped with a stderr warning** and listed in snapshot JSON as `skipped_large`. Prevents OOM today; streaming store lands with compression in Plan 04 (A4) where the write path is rewritten anyway.

### 5d · `GetActiveBranch` error swallow (S) — `db.go:482-492`

Distinguish `sql.ErrNoRows` (→ `"main"`, correct) from real errors (→ propagate). Callers already handle errors from `GetActiveBranchID`.

### 5e · Workspace `.avcignore` (S) — `snapshot.go:66`

`snapshot.Create` loads ignore rules from `projectRoot` even when walking a workspace. Load from `sourceDir` when it differs (fall back to project root if the workspace copy is absent).

### 5f · Web server hardening (M) — `web/server.go`, `cmd/avc/ui.go`

Prerequisite for Plan 06 C4.

- **Token auth:** generate a random token at startup, include it in the opened URL (`?token=…`); the UI stores it and sends `Authorization: Bearer` on every `/api/` call. Non-matching requests → 401.
- **Origin/Host check:** reject `/api/` requests whose `Origin`/`Host` isn't the bound address (blocks CSRF and DNS rebinding even if the token leaks into logs).
- Mutating endpoints already require POST — keep that, it's necessary but not sufficient.
- Tests: request without token → 401; forged-Origin POST → 403; happy path with token → 200.

### 5g · MCP transport limits (S) — `mcp/server.go:54-58`

Raise the scanner buffer to 32 MB and, on `bufio.ErrTooLong`, return a JSON-RPC error and continue the loop instead of exiting silently. (Protocol-revision upgrade is Plan 06 C3 — don't mix transport fixes with spec migration.)

### 5h · Repo hygiene (S) — repo root

`git rm --cached avc/avc_test.exe avc/avc_test_bin.exe`, add `*.exe` to `.gitignore` (scoped `avc/*.exe` so goreleaser artifacts elsewhere are unaffected).

### 5i · Migration error handling (S) — `db.go:263-268`

Replace `_, _ = s.db.Exec(ALTER TABLE …)` with a helper that ignores only "duplicate column name" errors and propagates everything else:

```go
func execIgnoreDuplicateColumn(db *sql.DB, stmt string) error
```

### 5j · Runner: output overflow must not convert success into timeout-kill (S) — `workspace/runner.go:158-199`

Once the `LimitedReader` cap is hit, the drain goroutine stops reading; the child eventually blocks on a full pipe, never exits, and is killed by the context timeout — a chatty-but-*passing* command is reported as exit `-1` after the full timeout. This would corrupt `avc bisect` verdicts (Plan 05 B2) with false "bad" results.

Fix: after the limit is consumed, keep draining the pipe to `io.Discard` so the child can finish normally:

```go
go func() {
    defer wg.Done()
    io.Copy(&stdoutBuf, stdoutReader) // capped capture
    io.Copy(io.Discard, stdoutPipe)   // keep the pipe drained so the child can exit
}()
```

Same for stderr. Track a `truncated` flag per stream (replaces the `remaining == 0` heuristic in `appendTruncationNote`).

**Test:** command emitting 2× the cap then exiting 0 → result within ~1 s, `exit_code: 0`, truncation note present.

### 5k · Runner docs: policy, not containment (S) — `mcp/tools.go`, `docs/workspace-command-runner.md`

The runner's blocklist inspects only the first command token, `PATH` allowlisting still admits `sh`/`bash`/`env`, and there is no filesystem or network isolation — it protects against *accidental* host pollution by cooperative tools, not against adversarial commands. Nothing in the tool description or docs says so today.

- Add to the `avc_run_in_workspace` tool description and `docs/workspace-command-runner.md`: *"The sandbox is a hygiene layer (env scrubbing, timeouts, workspace-scoped installs), not a security boundary. Commands run with the invoking user's full filesystem and network access. Do not use it to run untrusted code."*
- Rename the misleading `sandbox_info.layers` labels if needed so they describe what each layer actually is (e.g. `env_scrubbing`, not `isolation`).
- Real OS-level containment is [Plan 07](07-sandbox-containment.md).

---

## Exit criteria

- [x] Retention never deletes branch bases, tagged snapshots, or last-merge references (tests for each)
- [x] `avc delete` refuses protected snapshots without `--force`; MCP delete has no force path
- [x] GC skips objects younger than the grace window; `--grace` flag documented
- [x] Binary files report `binary: true`; >2,000-line files return estimated counts instantly
- [x] Branch delete refuses the DB-active branch
- [x] `**` and `!` work in `.avcignore` with table-driven tests
- [x] Exec bits survive snapshot→restore on Unix; symlink behavior documented
- [x] Files > threshold skipped with warning, listed in JSON
- [x] Web `/api/` requires bearer token and passes Origin/Host checks (401/403 tests)
- [x] MCP survives an oversized line with a JSON-RPC error instead of exiting
- [x] No `.exe` files tracked in git
- [x] Migration failures other than duplicate-column propagate
- [x] Runner: over-cap output returns promptly with the real exit code and a truncation note (no timeout-kill)
- [x] `avc_run_in_workspace` description and runner docs state the policy-not-containment limits explicitly
- [x] `go test ./...` green; docs updated

### Implementation notes (deviations and scope decisions)

- **5h (repo hygiene) was a non-issue.** `git ls-files` confirmed `avc/avc_test.exe` and `avc/avc_test_bin.exe` were never actually tracked — they exist only as local build artifacts already correctly matched by the top-level `*.exe` gitignore rule. The original review's claim was inaccurate; no action was needed.
- **Symlinks are explicitly out of scope for this pass.** They are currently dereferenced and copied as regular files on both snapshot and restore (via `os.Stat`/`os.ReadFile`, which follow symlinks) — a pre-existing limitation, not a regression introduced here. Proper symlink preservation (storing the link target as content with a mode bit, `os.Lstat`/`os.Symlink` on restore) is meaningful additional surface area, especially on Windows where symlink creation needs elevated privileges or Developer Mode; given the effort/risk tradeoff for this already-large plan, it was deferred rather than half-implemented. What *did* ship: Unix executable-bit preservation (`files.file_mode` column, applied via `os.Chmod` on restore) and empty-directory cleanup after a restore's quarantine sweep.
- **Web auth token delivery uses a cookie, not a URL parameter.** The plan sketched `?token=` in the opened URL; the shipped design instead sets a `SameSite=Strict` cookie on first page load (readable by the frontend JS, attached as `Authorization: Bearer` on every `/api/` call) so `avc ui`'s printed URL and browser-open behavior needed no changes. The server still accepts `?token=` as a fallback for scripted/curl access. Origin validation was added exactly as planned.
- **GC's default grace period required updating five existing tests** (`storage_test.go`, `data_safety_test.go`) that asserted immediate deletion of freshly-created objects — they now call `gc.RunWithGrace(..., 0)` explicitly to keep testing exact-count behavior, since production code correctly no longer collects objects younger than 15 minutes by default.
- **The `binary`/`counts_estimated` diff fields required touching six separate JSON call sites** (`cmd/avc/diff.go`, `diff_current.go`, `status.go`, `branch.go`, two spots in `internal/mcp/handlers.go`, two in `internal/web/server.go`) since each surface hand-rolls its own `fileDiffJSON`-shaped struct or map rather than sharing one — mechanical but broad; a future cleanup could consolidate these into one exported type in package `diff`.
