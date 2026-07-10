# AVC — Codebase Review & Feature Roadmap

**Date:** 2026-07-09
**Status:** Proposed
**Scope:** Full read of `avc/internal/`, `avc/cmd/avc/`, and `extension/src/`. Three critical findings were verified by live reproduction against the released binary.
**Implementation plans:** every item in this report is broken into six sequenced plans under [plans/](plans/00-overview.md).
**Relationship to prior work:** [improvement-plan.md](improvement-plan.md) (2026-05-23) is essentially fully implemented — WAL + indexes, branch-name validation, `project_state`, gc, retention, hooks, `internal/api/`, tags, export/import, and MCP tool tiers all exist in the code today. This review covers what that plan missed and what should come next.

---

## Part 1 — Critical bugs (verified by reproduction)

### 1.1 · `avc restore` deletes ignored files — data loss ⚠️

**Severity:** P0 · **Files:** [restore.go:84-104](../avc/internal/restore/restore.go#L84-L104)

`RestoreToDir` walks the target directory and removes every file not present in the target snapshot. It never loads `.avcignore`. Ignored files — `.env`, `node_modules/`, build outputs, local databases — are by definition never in any snapshot, so **every restore deletes them**.

Reproduced:

```
$ echo "*.env" >> .avcignore
$ echo "SECRET_KEY=super-secret" > prod.env
$ avc snapshot "baseline"
$ avc restore snap-8f108ac7297d
{"restored_files":2, "success":true}
$ ls
code.txt            # prod.env is gone
```

This is the worst possible failure mode for a tool whose pitch is safety: the recovery command destroys the files users deliberately excluded from tracking. The MCP `avc_restore` tool has the same behavior in workspaces.

**Fix:** load `.avcignore` in `RestoreToDir` and skip ignored paths in the deletion sweep (mirror the skip logic in `WalkProject`). Defense in depth: move deleted untracked files to `.avc/trash/<timestamp>/` instead of `os.Remove`, with `avc trash empty` for cleanup.

### 1.2 · `avc merge` panics when the branch deleted a file ⚠️

**Severity:** P0 · **Files:** [merge.go:121](../avc/internal/merge/merge.go#L121), [restore.go:157](../avc/internal/restore/restore.go#L157)

A file deleted on the branch (present in base and main, absent at branch HEAD) gets decision `"clean"` with `BranchHash == ""`. The apply loop calls `restore.ReadObject(projectRoot, "")`, and `objectPath` slices `hash[:2]` on an empty string:

```
$ rm .avc/workspaces/delbranch/b.txt
$ avc snapshot "deleted b.txt"
$ avc merge delbranch
panic: runtime error: slice bounds out of range [:2] with length 0
  ...internal/restore.objectPath(...)  restore.go:157
  ...internal/merge.Merge(...)         merge.go:121
```

Three distinct problems stack up here:

1. **The crash itself** — no guard on empty hashes.
2. **Deletion semantics are unimplemented** — even without the panic, merge has no code path that deletes a file from main. A branch that removes dead code can never propagate that removal.
3. **Corrupt merge state** — the panic fires *after* the `in_progress` merge row is inserted (merge.go Phase 3), so the DB permanently records a merge that never finished.

**Fix:** in the apply loop, treat `BranchHash == ""` as *delete the file from main* (`os.Remove` + record decision). Wrap the apply loop so any failure marks the merge row `failed` instead of leaving `in_progress`.

### 1.3 · `avc merge --abort` can never find the merge to abort

**Severity:** P0 · **Files:** [merge.go:221](../avc/internal/merge/merge.go#L221), [db.go:994-1009](../avc/internal/db/db.go#L994-L1009)

`Abort` looks up the last merge with `store.GetLastMerge(mainBranch.ID)`, but `InsertMerge` records `BranchID: agentBranch.ID`. The query matches nothing, so abort always returns *"no merge in progress to abort."* The test suite explicitly documents this as known-broken behavior ([merge_test.go:232-285](../avc/tests/merge_test.go#L232-L285)) rather than failing on it.

Combined with 1.2 this is an unrecoverable stuck state: a merge panics mid-apply, main is half-written, the merge row says `in_progress`, and the one command designed to recover from exactly this situation cannot locate the record.

**Fix:** either query merges by `project_id` ordered by `started_at DESC`, or store both branch IDs. Then un-quarantine `TestMerge_Abort_AfterConflict` so it asserts success.

### 1.4 · Hardlinked workspaces alias the real project root ⚠️

**Severity:** P0 · **Files:** [branch.go:56-101](../avc/internal/branch/branch.go#L56-L101), [copyopt_common.go](../avc/internal/branch/copyopt_common.go)

When a branch is created before main has any snapshot, `copyToWorkspace` hardlinks files into the workspace. The code comment claims this is safe because "AVC workspaces are written by avc_restore (which always creates a new file)" — but agents write to workspaces with ordinary tools (editors, `>>`, `sed -i`, scripts), and any in-place write mutates the shared inode:

```
$ echo "original" > data.txt
$ avc branch create hardlink-branch        # no snapshot on main yet
$ echo "MUTATED IN WORKSPACE" >> .avc/workspaces/hardlink-branch/data.txt
$ cat data.txt
original
MUTATED IN WORKSPACE                        # project root modified
```

This silently breaks the core promise of workspace isolation — the exact guarantee that makes AVC trustworthy for agents.

**Fix:** never hardlink into workspaces. Use reflink (true copy-on-write) where the filesystem supports it, byte-copy otherwise. Hardlinks are only safe for the read-only object store, not for a directory agents write to.

*(Note: the CLI path is the exposed one; the MCP `avc_branch_create` handler auto-snapshots first, which is why this went unnoticed.)*

---

## Part 2 — High-priority correctness issues (from code reading)

### 2.1 · Object store writes are not atomic and never verified

**Files:** [restore.go:177-186](../avc/internal/restore/restore.go#L177-L186)

`StoreObject` writes blobs with a plain `os.WriteFile`. A crash or disk-full mid-write leaves a truncated object, and the `os.Stat` existence check then treats it as already-stored **forever** — every future snapshot of that content dedupes against the corrupt blob. There is also no hash verification on read, so corruption surfaces as silently wrong file content on restore.

**Fix:** write to `<path>.tmp` then `os.Rename` (the statcache already does this correctly). Add `avc fsck` to re-hash all objects and report/quarantine mismatches.

### 2.2 · Snapshot creation is not transactional

**Files:** [snapshot.go:169-174](../avc/internal/snapshot/snapshot.go#L169-L174)

`InsertSnapshot` and `InsertFilesBatch` run as separate statements/transactions. A crash between them leaves a snapshot row with zero file rows — which restores as "delete everything" (see 1.1). Wrap both in one transaction.

### 2.3 · Retention and manual delete can destroy merge bases and tagged snapshots

**Files:** [retention.go:50-76](../avc/internal/retention/retention.go#L50-L76), [db.go:191-199](../avc/internal/db/db.go#L191-L199)

`branches.base_snapshot_id` and the three snapshot references in `merges` have **no FK constraints**, and `retention.Enforce` / `avc delete` prune blindly. Deleting a snapshot that is an active branch's merge base makes `buildPlan` read an empty base file list — every branch edit then looks like "added in both" and the merge produces spurious conflicts or wrong clean-merges. Retention also happily deletes snapshots tagged `stable`.

**Fix:** retention must exempt (a) any snapshot referenced as `base_snapshot_id` by an active branch, (b) tagged snapshots, (c) snapshots referenced by the most recent merge per branch. Add FK constraints (or a pre-delete check) for manual `avc delete`.

### 2.4 · GC races in-flight snapshots

**Files:** [gc.go:29-45](../avc/internal/gc/gc.go#L29-L45)

Snapshot creation stores objects *before* inserting DB rows. GC computes the live-hash set, closes the DB, then deletes — an object written by a concurrent snapshot in that window is collected, and the snapshot then references a missing blob. Retention triggers GC automatically after every snapshot when `auto_gc = true`, so two agents snapshotting concurrently can hit this in normal operation.

**Fix:** grace period — skip objects with mtime younger than a few minutes (git does the same). Longer-term, a `.avc/lock` advisory file lock for gc/merge/restore.

### 2.5 · Diff line-counting is unbounded O(m·n)

**Files:** [diff.go:192-198](../avc/internal/diff/diff.go#L192-L198)

`ComputeEdits` (the preview) is capped at 2,000 lines, but `lcsLength` — called for **every** modified file to produce line counts — has no cap. Two versions of a 300k-line generated file or log cost ~9×10¹⁰ comparisons; `avc diff`, `avc status`, and the extension sidebar all hang. There is also no binary-file detection, so binaries are diffed as text lines.

**Fix:** apply the same cap to `lcsLength` (fall back to `added = len(new), removed = len(old)` estimates); detect binary content (NUL byte in first 8 KB) and report `binary` instead of line counts.

### 2.6 · No `busy_timeout` — concurrent writers get hard SQLITE_BUSY errors

**Files:** [db.go:120-132](../avc/internal/db/db.go#L120-L132)

WAL allows readers during a write, but writer–writer contention still fails immediately without `PRAGMA busy_timeout`. AVC's normal deployment has up to four concurrent writers (CLI, MCP server, VSCode extension polling, web UI). Add `PRAGMA busy_timeout=5000` to the pragma list — one line.

### 2.7 · Merge merges the branch HEAD *snapshot*, not the workspace

**Files:** [merge.go:88-91](../avc/internal/merge/merge.go#L88-L91)

Edits made in the workspace after the last `avc_snapshot` are silently absent from the merge, and the workspace is then deleted on success (`RemoveWorkspace`) — un-snapshotted work is destroyed. **Fix:** before building the plan, compare workspace against HEAD (the `diff.CompareWithCurrentDir` machinery already exists); auto-snapshot if dirty, or refuse with a clear message.

### 2.8 · `avc restore` has no safety net of its own

The tool that exists to make everything reversible performs its most destructive operation irreversibly: restoring throws away the current working state (including, per 1.1, untracked files). All the infrastructure for `pre-restore: auto-snapshot` already exists — merge does exactly this. Snapshot current state before every restore and print the undo command.

### 2.9 · Branch delete checks the wrong active-branch source

**Files:** [branch.go:236-242](../avc/internal/branch/branch.go#L236-L242)

`Delete` reads the active branch from `config.toml`, but since Phase 7.3 the DB `project_state` table is authoritative (`GetActiveBranchName` prefers it). If the two disagree — the exact race `project_state` was built to fix — the active branch can be deleted along with its workspace. Use `GetActiveBranchName` instead.

### 2.10 · Smaller items

| # | Issue | Files |
|---|-------|-------|
| a | `.avcignore` docs/comments advertise `**` but `filepath.Match` doesn't support it; no `!` negation; bare-name patterns match any path component (surprising) | [fileutil.go:137-151](../avc/internal/fileutil/fileutil.go#L137-L151) |
| b | File modes not preserved — restore writes everything 0644; executable bits lost on Unix; symlinks are followed and flattened; empty dirs untracked and orphaned dirs left after restore | restore, fileutil |
| c | Whole files read into memory (`os.ReadFile`) for hash/store/restore — no streaming, no large-file cap or warning | fileutil, restore |
| d | `GetActiveBranch` returns `"main"` on **any** DB error, not just missing rows — a transient failure silently retargets snapshots to the wrong source dir | [db.go:482-492](../avc/internal/db/db.go#L482-L492) |
| e | Workspace snapshots load `.avcignore` from the project root, ignoring the workspace's own (possibly edited) copy | [snapshot.go:66](../avc/internal/snapshot/snapshot.go#L66) |
| f | Web UI: no auth token and no Origin/Host validation on mutating endpoints — any website can fire `fetch("http://127.0.0.1:3004/api/restore", {method:"POST"})` from the user's browser (CSRF/DNS-rebinding). Localhost bind is not sufficient | [server.go:23-61](../avc/internal/web/server.go#L23-L61) |
| g | MCP stdin scanner capped at 4 MB per line — `avc_resolve_conflict` with large `content` kills the request; protocol rev `2024-11-05` predates structured tool output | [server.go:54-58](../avc/internal/mcp/server.go#L54-L58) |
| h | `avc/avc_test.exe` and `avc/avc_test_bin.exe` are committed to git — add `*.exe` to `.gitignore` and remove | repo root |
| i | Migration `ALTER TABLE` errors are unconditionally discarded (`_, _ =`) — a real failure (disk full, locked DB) is indistinguishable from "column exists". Check the error string for "duplicate column" | [db.go:263-268](../avc/internal/db/db.go#L263-L268) |

### Suggested fix order

1. **Week 1 (data safety):** 1.1 restore-ignores, 1.4 hardlinks, 2.1 atomic objects, 2.2 snapshot transaction, 2.6 busy_timeout — all small, all load-bearing.
2. **Week 2 (merge integrity):** 1.2 deletion merge + panic, 1.3 abort lookup, 2.7 dirty-workspace guard, 2.8 pre-restore snapshot.
3. **Week 3 (lifecycle):** 2.3 retention exemptions, 2.4 GC grace period, 2.5 diff caps + binary detection, 2.9, and the 2.10 list.

Every Part 1 item should land with a regression test — the deletion-merge and ignored-file-restore scenarios above are ready-made test cases.

---

## Part 3 — Game-changer features

AVC's differentiation is *trust*: it is the only VCS designed around the question "what did the agent just do, and can I undo it?" The features below are ordered by how directly they compound that advantage.

### Tier A — Trust primitives (do these first)

**A1 · Line-level three-way merge.**
Today any file edited on both main and a branch is a whole-file conflict ([merge.go:340-352](../avc/internal/merge/merge.go#L340-L352) compares hashes only). For multi-agent workflows — AVC's headline use case — this makes merge nearly useless the moment two agents touch the same file, even in different functions. The building blocks already exist: `ComputeEdits` produces LCS edit scripts, and `writeConflict` already emits diff3 markers. Implement content-level diff3 (merge non-overlapping hunks automatically, conflict only on overlapping hunks). This is the single highest-leverage change in the codebase.

**A2 · Protected paths policy.**
A `[protect]` section in config listing globs that agent-attributed operations may not modify:

```toml
[protect]
paths = [".github/workflows/**", "secrets/**", "*.pem", "Makefile"]
mode  = "block"   # or "warn"
```

Enforced at merge time (refuse to apply changes touching protected paths without a human-set override) and surfaced in `avc_snapshot`/`avc_branch_diff` output. No other VCS offers "guardrails as a first-class feature"; for teams nervous about agent autonomy this is the reason to adopt AVC. The `run.enabled` mechanical-approval pattern already proves out the enforcement model.

**A3 · Universal undo.**
`avc undo` (restore the snapshot before the last state-changing operation) and `avc trash` (deleted untracked files land in `.avc/trash/`, recoverable). Combined with pre-restore auto-snapshots (2.8), every AVC operation becomes reversible — the marketing sentence writes itself.

**A4 · Integrity: `avc fsck` + object compression.**
Re-hash and verify every blob; quarantine mismatches. While touching the object store, add zstd compression (objects are currently raw bytes) — source code compresses 3–5×, directly addressing the storage growth that continuous snapshotting (B1) will create. Store a one-byte format header so old raw objects remain readable.

### Tier B — Agent-era killer features

**B1 · `avc watch` — the flight recorder.**
A daemon that watches the project (and workspaces) and takes debounced auto-snapshots on change bursts, tagged `auto:watch`. Retention already exists to bound growth; the statcache makes idle passes nearly free. This turns AVC into a time machine: *every* state an agent ever produced is recoverable, even when the agent forgot to snapshot. No mandate-following required — safety becomes structural instead of behavioral. Pair with `avc timeline` (see B3) for the review experience.

**B2 · `avc bisect` — automated regression hunting.**
Binary-search the snapshot history with a test command: `avc bisect --branch main --cmd "go test ./..."` restores midpoint snapshots into a scratch workspace, runs the command via the existing sandboxed runner, and reports the first bad snapshot plus its diff. Exposed as an MCP tool, this lets an agent answer "which change broke the build?" in O(log n) test runs — a task agents are currently terrible at. All the pieces (RestoreToDir, workspace.Run, snapshot ordering) already exist.

**B3 · Session attribution + AI change summaries.**
Snapshots already carry `agent_name`; add `session_id` and `task` columns so every snapshot links to the agent conversation that produced it. Then `avc timeline` renders the story of a work session: which agent, which task, what changed, in what order — the "what did my agents do while I slept" report. The `diffs.change_summary` column exists in the schema today and is never populated; fill it with generated summaries (heuristic first — "renamed X, added tests for Y" — optional LLM later) and surface them in `avc list`, the extension sidebar, and the web UI.

**B4 · Merge queue for agent fleets.**
When 3+ agents finish concurrently, merging is serial and manual. Add: branch-from-branch (stacked work), `avc merge --train <b1> <b2> <b3>` (merge sequentially, re-basing each preview on the previous result, running a validation hook between merges, stopping on first conflict), and cross-branch diff (`avc branch diff a..b`). This is the feature that makes "run five agents in parallel" an actual workflow instead of a demo.

### Tier C — Ecosystem & adoption

**C1 · Git bridge.**
Teams live in git; AVC must coexist, not compete. `avc git sync` exports snapshots as commits on a shadow branch (`refs/avc/<branch>`) with snapshot metadata in trailers, so agent history becomes visible in normal git tooling, PRs, and CI. Import direction: initialize AVC baselines from a git commit. This converts AVC from "another VCS to adopt" into "a safety layer on top of what you already use" — the single biggest adoption unlock.

**C2 · Remote sync.**
`avc push` / `avc pull` of the object store + DB deltas to S3 or SSH, building on the existing export/import machinery. Unlocks: agent history shared across machines, persistent history on ephemeral CI runners, and team-visible agent audit trails.

**C3 · MCP modernization.**
Adopt a current MCP revision: structured tool output (`outputSchema` — today every result is JSON-in-a-text-block that agents must re-parse), snapshots/diffs as MCP *resources* (browsable without tool calls), and elicitation for the merge approval flow so "user must approve merge" becomes a protocol-level prompt instead of an instruction the agent may ignore. Raise the 4 MB stdin cap while in there.

**C4 · Web UI as the merge cockpit.**
After hardening (2.10f: bearer token + Origin check), grow the web UI into the human review surface: side-by-side conflict resolution with ours/base/theirs panes (the diff3 data is already stored per merge-file), one-click resolve, and the B3 timeline. Non-VSCode users currently have no good way to review agent work — this is their front door.

### Sequencing

| Phase | Contents | Theme |
|-------|----------|-------|
| **S** (stabilize) | Part 1 + Part 2, weeks 1–3 | Ship v0.2 that never loses data |
| **T** (trust) | A1 diff3 merge, A2 protected paths, A3 undo/trash, A4 fsck+zstd | "Every operation reversible, every agent bounded" |
| **U** (unfair advantage) | B1 watch, B3 attribution+summaries, B2 bisect, B4 merge train | The features no git wrapper can match |
| **V** (reach) | C1 git bridge, C3 MCP upgrade, C2 remote sync, C4 web cockpit | Meet teams where they are |

Part 1 fixes are a hard prerequisite for everything else: B1 multiplies snapshot volume (needs A4/2.1 integrity and 2.3 retention correctness), B4 multiplies merges (needs 1.2/1.3), and A2's promise is hollow while 1.4 lets workspace writes leak to the root.

---

*Review performed 2026-07-09. Reproductions run against `avc` (scoop install) on Windows 11; findings 1.1, 1.2, and 1.4 confirmed by execution, 1.3 confirmed by test-suite annotation, all others by source reading.*
