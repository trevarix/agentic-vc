# Plan 02 — Merge Integrity

**Covers review items:** 1.2, 1.3, 2.7, 2.8
**Goal:** Merge and restore never crash, never silently drop work, and every failure state has a working recovery path.
**Prerequisite:** Plan 01 complete (the empty-hash read guard from 01·3 is assumed).
**Estimated duration:** ~1 week

---

## 1 · Deletion-aware merge + no more panic (review 1.2)

**Priority:** P0 · **Effort:** M
**Files:** `avc/internal/merge/merge.go:110-135` (apply loop), `merge.go:334-361` (decision table), `avc/internal/mcp/handlers.go` (result shape)

### Problem

A file deleted on the branch (`BranchHash == ""`, decision `"clean"`) makes the apply loop call `restore.ReadObject(projectRoot, "")` → slice panic. Verified by reproduction (review §1.2). Beyond the crash, merge has no deletion semantics at all — a branch can never remove a file from main. The panic also fires after the `in_progress` merge row is inserted, leaving corrupt merge state (see item 2).

### Implementation

**Step 1 — decision table.** Keep the hash-only decisions but make the deletion cases explicit so the apply loop can branch on them. The existing table already routes these correctly at the hash level; add a clarifying decision value instead of overloading `"clean"`:

| base | main | branch | today | new decision |
|------|------|--------|-------|--------------|
| h | h | `""` (deleted on branch) | `clean` → panic | `delete` |
| h | `""` (deleted on main) | h | `conflict` | `conflict` (unchanged; theirs section shows branch content) |
| h | h′ | `""` | `conflict` | `conflict` (delete-vs-edit; conflict file shows base/main with empty theirs) |
| `""` | `""` | h (added on branch) | `clean` | `clean` (unchanged) |

Concretely, in `buildPlan`'s switch: `case bh == mh && rh == "": decision = "delete"`. This case currently falls into `bh == mh → "clean"`, so the new arm must precede it.

**Step 2 — apply loop.**

```go
case "delete":
    if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
        return nil, fmt.Errorf("delete %s: %w", f.Path, err)
    }
    removeEmptyParents(projectRoot, dest)   // don't leave orphaned dirs
case "clean":
    // unchanged, but BranchHash is now guaranteed non-empty here
```

**Step 3 — everything downstream that switches on decision** must learn `"delete"`: `summarise` (count deletes as clean-equivalent or a separate `Deleted` counter — add `deleted` to the JSON), `mergeResultToMap` in the MCP handler, `merge_files.decision` values (TEXT column, no migration needed), and the CLI/web renderers.

**Step 4 — conflict writer with empty sides.** `writeConflict` already tolerates empty content per side; add explicit header text (`||||||| base (file deleted on branch)`) so delete-vs-edit conflicts are self-explanatory.

### Tests

- Regression: the review repro (branch deletes `b.txt`, merge) — asserts no panic, `b.txt` gone from main, decision `delete` recorded, merge status `completed`.
- Delete-vs-edit both directions → conflict, resolvable via `avc_resolve_conflict` with `"ours"`/`"theirs"` (theirs = deletion must remove the file).
- Post-merge snapshot after a deletion merge does not contain the deleted path.

---

## 2 · Merge failure never leaves `in_progress`; abort works (review 1.3)

**Priority:** P0 · **Effort:** M
**Files:** `avc/internal/merge/merge.go:56-248`, `avc/internal/db/db.go:994-1009`, `avc/tests/merge_test.go:232-285`

### Problem

Two halves:

1. `Abort` calls `store.GetLastMerge(mainBranch.ID)`, but `InsertMerge` stores `BranchID: agentBranch.ID` — the query never matches, so abort always fails with "no merge in progress." The test suite documents this as known-broken.
2. Any error (or the 1.2 panic) between `InsertMerge` and `UpdateMergeStatus` leaves the row `in_progress` forever, and half-written files on main.

### Implementation

**Step 1 — project-scoped merge lookup.**

```go
// GetLastMergeForProject returns the most recent merge for a project, any branch.
func (s *Store) GetLastMergeForProject(projectID string) (*Merge, error) {
    // SELECT ... FROM merges WHERE project_id = ? ORDER BY started_at DESC, rowid DESC LIMIT 1
}
```

`Abort` uses this instead of `GetLastMerge(mainBranch.ID)`. Keep `GetLastMerge(branchID)` for `resolve.go`, which correctly queries by agent branch.

**Step 2 — failure marks the merge `failed`.** Restructure `Merge` so the apply loop runs inside a helper whose error path updates status before returning:

```go
applyErr := applyPlan(projectRoot, files) // the current loop, extracted
if applyErr != nil {
    markMergeStatus(projectRoot, mergeID, "failed")   // own open/close, best-effort
    return nil, fmt.Errorf("merge apply failed (main may be partially written — run `avc merge --abort` to roll back): %w", applyErr)
}
```

Add `"failed"` to the documented status set (`in_progress | completed | conflicts | aborted | failed`) and let `Abort` accept it: `m.Status != "in_progress" && m.Status != "conflicts" && m.Status != "failed" → error`.

**Step 3 — un-quarantine the tests.** Rewrite `TestMerge_Abort_AfterConflict` to assert success: after abort, conflict markers gone, main byte-identical to the pre-merge snapshot, merge row status `aborted`. Add `TestMerge_Abort_AfterFailure` using error injection.

### Tests

As step 3, plus: abort with no merge history still errors cleanly; abort after a *completed* merge refuses ("nothing to abort").

---

## 3 · Dirty-workspace guard on merge (review 2.7)

**Priority:** P1 · **Effort:** S
**Files:** `avc/internal/merge/merge.go:56-70`, `avc/internal/diff/diff_current.go`

### Problem

`Merge` merges the branch's HEAD *snapshot*. Workspace edits made after the last snapshot are silently absent from the merge — and then destroyed when `RemoveWorkspace` deletes the workspace on success.

### Implementation

At the top of `Merge` (and `Preview`, so the preview matches what would merge), compare the workspace against branch HEAD using the existing `diff.CompareWithCurrentDir`. If dirty, auto-snapshot first — this matches the tool's ethos (snapshots are cheap; silent loss is not):

```go
ws := branch.WorkspacePath(projectRoot, branchName)
if ws != "" {
    if dirty, n := workspaceDirty(projectRoot, ws, headSnap.ID); dirty {
        snap, err := snapshot.Create(projectRoot,
            fmt.Sprintf("auto: pre-merge workspace state (%d changed)", n),
            "avc-merge", "un-snapshotted workspace changes captured before merge",
            agentBranch.ID, ws)
        if err != nil {
            return nil, fmt.Errorf("workspace has un-snapshotted changes and auto-snapshot failed: %w", err)
        }
        // re-resolve headSnap = snap
    }
}
```

Surface `auto_snapshot_id` in the merge result JSON so agents/users can see it happened.

### Tests

Edit a workspace file without snapshotting → merge → merged main contains the edit; result includes `auto_snapshot_id`. Clean workspace → no extra snapshot created.

---

## 4 · Pre-restore safety snapshot (review 2.8)

**Priority:** P1 · **Effort:** S
**Files:** `avc/internal/restore/restore.go:29-54`, `avc/internal/mcp/handlers.go:224-248`

### Problem

Restore discards the current working state irreversibly — the one destructive operation in a tool whose purpose is reversibility. Merge already auto-snapshots before mutating; restore must too.

### Implementation

In `restore.Restore` (CLI/main path) and in the MCP `toolRestore` workspace path, before calling `RestoreToDir`:

```go
pre, err := snapshot.Create(projectRoot,
    fmt.Sprintf("pre-restore: before restoring %s", snapshotID),
    "avc-restore", "automatic safety snapshot", activeBranchID, sourceDir)
```

Rules:

- **Skip when clean:** if the working tree already matches branch HEAD (same `workspaceDirty` check as item 3), don't create a duplicate snapshot.
- **Skip when restoring to the snapshot you just took** (no-op restores).
- Failure of the safety snapshot **aborts the restore** (unlike post-hooks) — restoring without a way back defeats the point.
- Print/return the undo hint: `To undo this restore: avc restore <pre-snapshot-id>` (CLI stderr text + `undo_snapshot_id` field in `--json` and MCP output).

Factor the shared "snapshot the active source dir" logic out of items 3 and 4 into one helper (one-concern rule) — both are "capture current state before a destructive op."

### Tests

Restore an older snapshot → a `pre-restore:` snapshot exists at previous HEAD; restoring `undo_snapshot_id` round-trips the working tree byte-identically. Clean-tree restore creates no extra snapshot.

---

## Exit criteria

- [x] Review §1.2 repro passes as a regression test: deletion merge completes, file removed from main, no panic
- [x] `delete` decisions appear in merge JSON, `merge_files` rows, CLI and web output
- [x] Delete-vs-edit conflicts are produced, rendered with labeled empty sides, and resolvable both ways
- [x] `avc merge --abort` succeeds after conflicts **and** after an injected apply failure; main restored byte-identical to pre-merge snapshot
- [x] No code path can exit `Merge` leaving status `in_progress`
- [x] `TestMerge_Abort_AfterConflict` asserts success (quarantine comments removed)
- [x] Dirty workspace is auto-snapshotted before merge; `auto_snapshot_id` surfaced
- [x] Every restore of a dirty tree creates a `pre-restore:` snapshot and returns `undo_snapshot_id`
- [x] `go test ./...` green; `docs/cli-reference.md` updated (merge statuses, undo hint)

### Implementation notes (deviations from the original plan)

- **Import-cycle constraint on item 4.** `internal/restore` cannot import `internal/snapshot` (snapshot already imports restore for `StoreObject`). The pre-restore safety snapshot is therefore implemented as a shared helper, `snapshot.CreateBeforeRestore` (new file `internal/snapshot/dirty.go`), called from each restore call site (`cmd/avc/restore.go`, `internal/mcp/handlers.go`, `internal/web/server.go`) rather than inside `restore.go` itself. This also satisfies the plan's own ask to factor the shared logic into one helper — `CreateIfDirty` is the low-level primitive, `CreateBeforeRestore` is the restore-specific wrapper, and both `merge.autoSnapshotDirtyWorkspace` (item 3) and all three restore surfaces (item 4) call into the same code.
- **Preview does not auto-snapshot.** The plan's item 3 says "and Preview, so the preview matches what would merge" — but `Preview`'s documented contract is "no writes, no recorded merge," and creating a snapshot as a side effect of a dry run would violate that. Preview instead reports `WorkspaceDirtyFiles` (read-only diff, no snapshot) so callers know the preview may be incomplete, without mutating anything.
- **Un-snapshotted branches now merge instead of erroring.** A pre-existing test (`TestMerge_ErrorWhenBranchHasNoSnapshots`) asserted that merging a branch with zero snapshots was an error. The item 3 dirty-workspace guard captures that branch's materialized-but-never-snapshotted state automatically instead (the same "always dirty when there's no history" rule `CreateIfDirty` documents), so the merge now succeeds. Replaced with `TestMerge_AutoSnapshotsUnsnapshottedBranchBeforeMerging`, asserting the new (safer) behavior.
- **Web restore hardening included.** Though not in the plan's stated file list, `internal/web/server.go`'s `restoreHandler` was also wired to `CreateBeforeRestore` for consistency — otherwise the web UI would be the one restore surface without the safety net. Its pre-existing lack of branch-awareness (it always restores to `projectRoot` regardless of active branch) was left as-is; that's a separate, unrelated gap.
