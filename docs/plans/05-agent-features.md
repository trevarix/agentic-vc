# Plan 05 — Agent-Era Features (Tier B)

**Covers review items:** B1 (`avc watch`), B2 (`avc bisect`), B3 (session attribution + change summaries), B4 (merge queue)
**Goal:** The capabilities no git wrapper can match — structural safety, automated regression hunting, auditability, and true parallel-agent workflows.
**Prerequisite:** Plans 01–04 complete. B1 specifically requires 03·1 (retention exemptions), 03·2 (GC grace), and 04·A4 (compression + fsck).
**Estimated duration:** ~4 weeks

**Suggested order within the plan:** B3 → B1 → B2 → B4. B3's schema additions (session/task columns) should exist before B1 starts generating high-volume snapshots that would need backfilling, and B4 is the largest and most separable item.

---

## B3 · Session attribution + change summaries

**Priority:** P1 · **Effort:** M
**Files:** `avc/internal/db/db.go`, `avc/internal/snapshot/snapshot.go`, `avc/internal/mcp/tools.go` + `handlers.go`, new `avc/cmd/avc/timeline.go`, `avc/internal/diff/`

### Problem

Snapshots record `agent_name` but nothing links them to *which session or task* produced them. The unused `diffs.change_summary` column was designed for human-readable summaries and is never populated. The result: `avc list` after a long agent session is a wall of `auto:` labels with no story.

### Implementation

**Step 1 — schema (idempotent ALTERs):** `snapshots.session_id TEXT`, `snapshots.task TEXT`. MCP `avc_snapshot` gains optional `session_id`/`task` args; the generated skills instructions (`internal/skills`) tell agents to pass a stable session ID and one-line task description. CLI: `avc snapshot --session --task`.

**Step 2 — heuristic change summaries.** On snapshot creation (and lazily for diffs), generate a one-liner from the file diff vs branch HEAD — no LLM required:

```
"3 files: modified auth.go (+40 -12), added auth_test.go, deleted legacy.go"
```

Store in `diffs.change_summary` (the column exists; start writing it). An optional LLM hook (`[summaries] command = "..."` in config, receiving the diff on stdin) can upgrade quality later — config-gated, off by default, never blocking.

**Step 3 — `avc timeline`:**

```
avc timeline                    # active branch, grouped by session
avc timeline --session <id>     # one session's story
avc timeline --json
```

Renders: session → task → ordered snapshots with summaries, merge/restore events interleaved from the Plan 04 op log. This is the "what did my agents do while I slept" report. Extension: reuse the existing `timelineViewer.ts` webview; web UI: `/api/timeline` (via `internal/api`).

### Tests

Session/task round-trip through MCP and CLI into list/timeline JSON. Summary generator table-tests (adds/deletes/modifies/renamed-looking pairs). Timeline groups correctly with interleaved ops.

---

## B1 · `avc watch` — continuous checkpointing

**Priority:** P0 (the flagship) · **Effort:** L
**Files:** new `avc/internal/watch/watch.go`, new `avc/cmd/avc/watch.go`, `avc/internal/config/config.go`

### Problem

Safety today is behavioral — it depends on agents remembering to call `avc_snapshot`. A watcher makes safety structural: every state the project ever passes through is recoverable, mandate-following or not.

### Design

```
avc watch                # foreground daemon; Ctrl+C to stop
avc watch --status       # is a watcher running for this project?
```

```toml
[watch]
debounce_seconds = 30    # quiet period after last change before snapshotting
min_interval_seconds = 120
include_workspaces = true
```

- **Watch targets:** project root (main) and each active branch workspace, honoring `.avcignore`.
- **Mechanism:** `fsnotify` (cross-platform, pure Go). Fallback `--poll <seconds>` mode using the statcache walk (which is already nearly free on idle trees) for network filesystems where fsnotify is unreliable.
- **Debounce:** change events start/reset a timer; on expiry, snapshot with label `auto:watch <summary>` (B3 summaries), `agent_name = "avc-watch"`, correct branch scoping via the same `sourceDir` logic as `toolSnapshot`.
- **Dedup:** skip if tree hash equals branch HEAD (statcache makes this cheap) — idle projects generate zero snapshots.
- **Volume control:** watch snapshots are the first candidates for retention; add `[retention] max_watch_snapshots_per_branch` (default 200) pruning `auto:watch` snapshots *first* while honoring the Plan 03 protected set. This is why 03·1 and 04·A4 are prerequisites.
- **Single-instance guard:** pid file under `.avc/watch.pid`; second invocation refuses.
- **Concurrency correctness:** watch writes race agent snapshots by design — this leans on Plan 01 (busy_timeout, transactional snapshot) and Plan 03 (GC grace). Add a soak test.

VSCode extension: `avc.watch.enabled` setting spawns/kills the watcher with the extension lifecycle (via `cliProxy`, per architecture rules); the existing `autoSnapshot.ts` becomes redundant and is removed if the CLI watcher is active.

### Tests

Burst of writes → exactly one snapshot after debounce. Idle → zero snapshots. Ignored-file churn (e.g. build output) → zero snapshots. Workspace edit snapshots to the right branch. Soak: watcher + concurrent manual snapshots for 30 s, DB consistent, no busy errors, `avc fsck` clean.

---

## B2 · `avc bisect` — automated regression hunting

**Priority:** P1 · **Effort:** M
**Files:** new `avc/internal/bisect/bisect.go`, new `avc/cmd/avc/bisect.go`, `avc/internal/mcp/tools.go` + `handlers.go`

### Problem

"Which change broke the tests?" is O(n) manual restores today. With ordered snapshots, a sandboxed runner, and cheap workspace materialization, it should be O(log n) and fully automatic — a task agents are currently bad at, handed to them as one tool call.

### Design

```
avc bisect --branch main --good snap-aaa [--bad snap-zzz] --cmd "go test ./..."
```

- Default `--bad` = branch HEAD; `--good` required (or `--good-tag stable` via the tags table).
- Materialize each candidate into a **scratch workspace** (`.avc/workspaces/.bisect-<id>/` — reuse `RestoreToDir`; the leading dot keeps it out of branch listings, and `ValidateBranchName` already rejects user branches with leading dots so there is no collision).
- Run `--cmd` through the existing `workspace.Run` sandbox (env scrubbing, timeout, output caps). **Requires `[run] enabled = true`** — same mechanical gate as `avc_run_in_workspace`, enforced in both CLI and MCP paths.
- Standard bisect loop on the snapshot's timestamp/rowid ordering; exit code 0 = good, non-zero = bad, special exit 125 = skip (unbuildable), like git.
- Output: first bad snapshot + its `avc diff` vs predecessor + B3 summary. `--json` streams per-step progress objects so agents can observe long runs.
- Cleanup: scratch workspace removed on completion or interrupt (defer + pid-style stale-dir sweep on next run).

MCP: `avc_bisect(cmd, good, bad?, branch?, timeout_seconds?)` (full tier only). Description mandates showing the user the command before calling — consistent with `avc_run_in_workspace` policy — and the `run.enabled` gate backs it mechanically.

### Tests

Seeded history of 16 snapshots with a known breaking snapshot → bisect finds it in ≤ 5 runs (command = a script checking for the bad marker file). Skip path (125) narrows correctly. Disabled `[run]` → refused. Scratch dir gone afterwards.

---

## B4 · Merge queue for agent fleets

**Priority:** P2 · **Effort:** L
**Files:** `avc/internal/branch/branch.go`, `avc/internal/merge/`, new `avc/internal/merge/train.go`, `avc/cmd/avc/merge.go`, `avc/cmd/avc/branch.go`

### Problem

With 3+ agents finishing concurrently, merges are serial and manual, and each merge invalidates the next branch's preview (its base is now stale). There is also no branch-from-branch (stacked work) and no cross-branch diff.

### Implementation

**Step 1 — cross-branch diff (S):** `avc branch diff <a>..<b>` — resolve each side's HEAD snapshot, reuse `diff.Compare`. MCP: extend `avc_branch_diff` with optional `against`.

**Step 2 — branch-from-branch (M):** `avc branch create <name> --from-branch <parent>` sets `base_snapshot_id` = parent HEAD. Merge of a child targets main as today (base already encodes the fork point — the three-way math is unchanged). Record `parent_branch_id` (idempotent ALTER) for display; `avc branch list` shows lineage.

**Step 3 — merge train (L):**

```
avc merge --train feat/a feat/b feat/c [--validate "go test ./..."]
```

Loop per branch, in order:

1. Preview against *current* main (each iteration sees the previous merges — this is the point of a train).
2. Conflicts → **stop**; report merged-so-far / stopped-at / remaining. Main keeps completed merges (each is individually recoverable via its pre-merge snapshot; the Plan 02 status machinery needs no changes).
3. Clean → merge (full Plan 02/04 pipeline: dirty-workspace guard, diff3, protected paths).
4. `--validate` set → run it via `workspace.Run` sandbox semantics against post-merge main; failure → auto-restore that merge's pre-merge snapshot, mark the branch back to `active`, stop, report.

Result JSON: per-branch `{branch, status: merged|conflicts|validation_failed|skipped, post_merge_snapshot_id}`.

MCP: `avc_merge_train(branches[], validate?)` — same explicit-approval rule as `avc_merge` (approval covers the whole train), `--validate` gated behind `run.enabled`.

### Tests

Train of 3 disjoint branches → all merge; overlapping second branch that diff3 can't resolve → stops with correct report; failing `--validate` → that merge rolled back, branch active again, main equals its own pre-merge snapshot; stacked branch merges cleanly after its parent merged via the train.

---

## Exit criteria

- [x] Snapshots carry `session_id`/`task` end-to-end (MCP → DB → list/timeline JSON)
- [x] `diffs.change_summary` populated; `avc timeline` renders sessions with summaries and interleaved ops
- [x] `avc watch`: debounced snapshots, zero on idle/ignored churn, correct branch scoping, single-instance guard, soak test green with `avc fsck` clean
- [x] Watch-snapshot retention prunes `auto:watch` first and never touches protected snapshots
- [x] `avc bisect` finds a seeded regression in O(log n) runs, honors skip, cleans up, and is gated on `[run] enabled`
- [x] `avc branch diff a..b`, stacked branches, and `avc merge --train` with `--validate` rollback all behave per tests
- [x] All new MCP tools registered in the correct tiers with accurate descriptions
- [x] `go test ./...` green; CLI reference + architecture docs updated

### Implementation notes (deviations and decisions)

- **B3's snapshot-level one-liner is composed, not stored.** `diffs.change_summary` is a per-file column, so each row gets a self-describing fragment ("modified auth.go (+40 -12)") and the snapshot-level line is composed from the cached rows (`diff.SummarizeCached`) — no new column, and `avc timeline` computes+caches summaries lazily for snapshots that predate the feature. Line counts during summary generation are capped at 50 modified files per snapshot so a huge changeset can't stall its own snapshot; the counts-only path also skips the unified-diff backtracking table entirely.
- **The diffs cache gained real rows — and two FK landmines with them.** `DeleteSnapshot`/`DeleteSnapshotsByBranch` now clear cache rows referencing the deleted snapshots first; the previously-empty table had been masking the constraint.
- **`avc watch` survives its own event storm.** fsnotify's backend can block event delivery while servicing `watcher.Add`, and the daemon adds watches for new directories from its main loop — draining events on that same goroutine deadlocked (caught by the burst test). A pump goroutine now drains `watcher.Events` into a buffered queue; overflow degrades to "mark everything dirty" (a cheap stat-cache re-check), never a lost change.
- **Watch liveness is mtime-heartbeat, not PID probing.** Portable to Windows; a pid file whose heartbeat is >90s old is treated as crashed and replaced.
- **Watch skips oversized files exactly as snapshots do** — otherwise a file over `max_file_size_mb` would read as a perpetual "change" and loop the daemon forever.
- **The extension manages the daemon** (`avc.watch.enabled` spawns/kills `avc watch` via cliProxy). `autoSnapshot.ts` was kept but stands down while the watcher runs — removing it entirely would break users who prefer the editor-only behavior without the CLI daemon.
- **Bisect refuses to guess on a -1 exit** (timeout or sandbox-blocked command): neither is a verdict about the snapshot, and misclassifying it would misdirect the whole search. Skips that prevent exact narrowing flag the result `ambiguous` instead of silently pointing at the wrong snapshot. Candidates are materialized fresh per step (no quarantine churn, no cross-step contamination).
- **The train stops on *preview* conflicts, before writing.** A conflict discovered only at merge time (dirty workspace auto-snapshotted in between) is aborted immediately so a stopping train never leaves conflict markers on main. Validation rollback reuses `undo.Undo` — the newest logged operation at that point is exactly the failed merge, so the op log stays coherent (a later manual `avc undo` behaves as redo).
- **`--validate` runs through `workspace.RunInDir`**, a directory-targeted variant of the sandbox runner extracted from `Run` (env scrubbing, blocklist, timeout, output caps), because validation must execute against post-merge main, which is not a workspace. Gated on `[run] enabled` like every command-execution surface; the MCP `avc_merge_train` cannot pass `--allow-protected` (CLI-only, as for `avc_merge`).
- **Status vocabulary gained two members** beyond the plan's list: `blocked_protected` (the [protect] gate stopped the train) and `error` (branch missing etc.) — folding either into "conflicts" would have been a lie.
- **`ListSnapshotsByBranch` now tiebreaks on rowid**, making same-second snapshot ordering true insertion order — bisect and timeline depend on it; `GetHeadSnapshot` already did this.
- **`avc_bisect` ships in the full MCP tier only; `avc_merge_train` likewise** (it needs the same explicit-approval discipline as `avc_merge`, and one approval covers the whole train). `avc_snapshot` gained `session_id`/`task`; `avc_branch_create` gained `from_branch`; `avc_branch_diff` gained `against`.
- **fsnotify pinned to v1.8.0** — keeps `go.mod` at Go 1.22 for CI, same policy as the zstd pin in Plan 04.
- **The timeline ships on three surfaces: CLI (`avc timeline`), web (`/api/timeline`), MCP-adjacent (snapshots carry the attribution).** Re-skinning the extension's existing snapshot-timeline webview around sessions was deferred to Plan 06's web-cockpit work, where that UI is being reworked anyway.
