# Plan 04 — Trust Primitives (Tier A)

**Covers review items:** A1 (diff3 merge), A2 (protected paths), A3 (universal undo), A4 (fsck + compression)
**Goal:** Every AVC operation is reversible, every agent is bounded, and stored history is verifiable. This is the release that earns the tagline.
**Prerequisite:** Plans 01–03 complete. A4 must land **before** Plan 05 B1 (continuous snapshotting).
**Estimated duration:** ~3 weeks

---

## A1 · Line-level three-way merge

**Priority:** P0 (highest-leverage change in the codebase) · **Effort:** L
**Files:** new `avc/internal/merge/diff3.go`, `avc/internal/merge/merge.go`, `avc/internal/diff/diff.go` (reuse `ComputeEdits`)

### Problem

Merge decisions are whole-file hash comparisons — any file edited on both main and branch is a full-file conflict, even when the edits touch different functions. For multi-agent workflows this makes merge nearly useless the moment two agents share a file.

### Design

Standard diff3: compute edit scripts base→main and base→branch (the existing LCS `ComputeEdits` provides these), align them into regions, then:

- Region changed only in main → take main.
- Region changed only in branch → take branch.
- Region changed identically in both → take either.
- Region changed differently in both → **conflict hunk** (markers around just that hunk, same diff3 format `writeConflict` uses today).

### Implementation

**Step 1 — `diff3.Merge`:**

```go
// Merge performs a line-level three-way merge.
// Returns the merged content and the number of conflict hunks (0 = clean).
func Merge(base, main, branch []string) (merged []string, conflicts int)
```

Pure function, no I/O — exhaustively table-testable.

**Step 2 — wire into the decision table.** In `buildPlan`, the current `default: conflict` arm becomes a content attempt:

```go
default: // all three hashes differ — try content-level merge
    merged, hunks := diff3Merge(projectRoot, bh, mh, rh)
    if hunks == 0 {
        decision = "merged"        // new decision: content produced, no object yet
    } else {
        decision = "conflict"      // apply loop writes the hunk-marked content
    }
```

Guards: fall back to whole-file `conflict` when any side is binary (Plan 03·3 detector) or exceeds `maxDiffFileLines` — never attempt line merges on binary content.

**Step 3 — apply + persist.** `"merged"` results are new content that exists in no snapshot: store the merged bytes as an object (`StoreObject`) during apply so the post-merge snapshot dedupes against it, and write it to main. Conflict files now contain *hunk-level* markers; `ListConflicts`' marker scan keeps working, and `avc_resolve_conflict`'s `ours`/`theirs` whole-file resolutions remain valid escape hatches.

**Step 4 — preview parity.** `Preview` runs the same diff3 pass so `clean/merged/conflict` counts match what `Merge` will do. This makes preview slower than hash comparison — acceptable; cap via the same size guards.

### Tests

Table-driven `diff3.Merge` suite: non-overlapping edits both sides → clean; adjacent-line edits → conflict; identical edits → clean; one side deletes a region the other edits → conflict; whole-file rewrite → single conflict hunk. Integration: two branches editing different functions of one file merge cleanly end-to-end; `merge_quality_test.go` gains the classic torture cases.

---

## A2 · Protected paths policy

**Priority:** P0 · **Effort:** M
**Files:** `avc/internal/config/config.go`, new `avc/internal/policy/policy.go`, `avc/internal/merge/merge.go`, `avc/internal/mcp/handlers.go`

### Problem

Nothing bounds what an agent may change. Teams need "agents may not touch CI config / secrets / the Makefile" as an enforced property, not a prompt instruction.

### Design

```toml
[protect]
paths = [".github/workflows/**", "secrets/**", "*.pem"]
mode  = "block"    # "block" | "warn"
```

Enforcement points (mechanical, like `run.enabled` — agents cannot lift them):

1. **Merge (the hard gate):** in `buildPlan`, any non-skip decision whose path matches a protected glob → merge refuses in `block` mode, listing the paths; `warn` mode annotates the result. Human override: `avc merge <branch> --allow-protected` — CLI only, **no MCP equivalent**.
2. **Snapshot (early warning):** `avc_snapshot` and `avc_branch_diff` responses include `protected_changes: [paths]` so agents learn they've strayed *before* merge time.
3. **Status:** `avc status` marks protected changed files with `!`.

Glob matching reuses the Plan 03·5a `**` matcher (one matcher, two configs — assert they share code).

### Implementation notes

`policy.Check(cfg, paths) → []Violation`; call sites format. Config docs must state plainly: protection applies to AVC-mediated integration (merge into main). An agent writing directly to the project root on main bypasses it — which is why the CLAUDE.md/skills instructions mandate branch workspaces; note this in the generated skills instructions (`internal/skills`).

### Tests

Merge with protected change blocks / warns / overrides per mode. MCP merge has no override path. Snapshot response lists protected changes. Empty `[protect]` = zero behavior change.

---

## A3 · Universal undo

**Priority:** P1 · **Effort:** M
**Files:** new `avc/cmd/avc/undo.go`, `avc/internal/trash/` (from Plan 01), new `avc/internal/oplog/oplog.go`, MCP tools

### Problem

Recovery today requires knowing which snapshot to restore. `avc undo` should make the last state-changing operation reversible with zero arguments.

### Design

**Op log.** A small `operations` table (idempotent migration): `id, project_id, branch_id, kind (restore|merge|snapshot|branch_delete), undo_snapshot_id, created_at, details JSON`. Plan 02 already creates the safety snapshots (`pre-restore:`, pre-merge); this plan records them uniformly:

- `avc undo` → restores `undo_snapshot_id` of the newest undoable op on the active branch, records *itself* as an op (so `avc undo` twice = redo).
- `avc undo --list` → recent ops with what undo would do.
- `avc trash restore <opID> [path]` → completes the Plan 01 trash story: recover untracked files the restore sweep removed.

MCP: `avc_undo` (S) — one obvious recovery verb for agents; description tells agents to prefer it over guessing snapshot IDs after a mistake.

### Tests

restore → undo → tree byte-identical to pre-restore. merge → undo ≡ abort semantics on a completed merge (restores pre-merge snapshot, marks branch active again). undo twice round-trips. Trash restore recovers an untracked file into place.

---

## A4 · Integrity: `avc fsck` + object compression

**Priority:** P1 (hard prerequisite for Plan 05 B1) · **Effort:** L
**Files:** `avc/internal/restore/restore.go` (object I/O), new `avc/internal/objstore/objstore.go`, new `avc/cmd/avc/fsck.go`

### Problem

Objects are raw bytes, never re-verified. A corrupt blob (pre-Plan-01 torn write, disk fault, manual tampering) silently restores wrong content. And continuous snapshotting (B1) will multiply store volume — raw source text wastes 3–5× disk.

### Design

**Step 1 — extract `internal/objstore`.** Object read/write currently lives in `restore` (with a duplicate reader in `diff`). Move to one package: `Store(root, hash, data)`, `Read(root, hash)`, `Exists`, `Walk`. Mechanical refactor, blast-radius contained before format changes.

**Step 2 — format v2 with a 1-byte header:** `0x00` = raw (legacy, headerless files are also raw), `0x01` = zstd. Write path compresses when it wins (skip already-compressed content by extension + entropy check); read path sniffs the header. `github.com/klauspost/compress/zstd` — pure Go, no CGO, consistent with the sqlite driver choice.

**Step 3 — `avc fsck`:**

```
avc fsck            # re-hash every object (decompressing v2), verify against filename
avc fsck --repair   # quarantine corrupt objects to .avc/corrupt/, list affected snapshots
avc fsck --json
```

Affected-snapshot mapping via the `files` table (`WHERE file_hash IN (…)`). Read-path verification stays off the hot path (hash-on-every-read doubles restore cost); fsck is the audit tool, and `avc export` runs an implicit fsck of the objects it bundles.

**Step 4 — `avc storage` gains** `compressed_bytes` / `raw_bytes` / ratio.

### Tests

Round-trip both formats; legacy raw objects read fine post-upgrade. fsck detects a deliberately corrupted blob and names the snapshots it damages. Compression skipped for a `.png`. GC handles both formats (sizes reported are on-disk sizes).

---

## Exit criteria

- [x] Two branches editing disjoint regions of the same file merge cleanly; overlapping edits conflict at hunk level only
- [x] Preview counts match merge outcomes (including `merged`)
- [x] Binary/oversized files never get line-merged
- [x] `[protect]` in `block` mode stops a merge touching protected paths; CLI `--allow-protected` overrides; MCP cannot
- [x] `avc undo` reverses the last restore and the last merge; `avc undo --list` works; `avc_undo` exposed via MCP
- [x] `avc trash restore` recovers swept untracked files
- [x] Objects round-trip through zstd; legacy objects still readable; `avc fsck` catches injected corruption and maps it to snapshots
- [x] `go test ./...` green; `docs/cli-reference.md` + `docs/architecture.md` document objstore v2, undo, protect, fsck

### Implementation notes (deviations and decisions)

- **Diff3 preserves bytes exactly.** The merge splits lines keeping their original terminators (`\r\n` vs `\n`) rather than reusing `diff.SplitLines` (which normalizes line endings for comparison). A clean merge must write byte-faithful content back to main — normalizing every line ending of an otherwise-untouched region would itself be data corruption. Consequence: a CRLF-vs-LF difference between sides is honestly a change, not silently papered over.
- **Merged content is computed once, in `buildPlan`.** Both the clean result and hunk-marked conflict content ride along in `FileResult` (unexported field), so Preview and Merge always agree and `applyPlan` never recomputes. Hunk markers reuse the exact `<<<<<<< main (ours)` format, so `ListConflicts`/`avc_resolve_conflict` work unchanged on hunk conflicts.
- **Adjacent-line edits conflict (like git).** With no common synchronization point between two touching edits, diff3 correctly reports a conflict rather than guessing — the plan's "adjacent-line edits" test case was written to match real diff3/git semantics.
- **v2 object format uses a magic header (`AVCO` + format byte + raw size), not a bare format byte.** A single leading byte can't be distinguished from legacy raw content that happens to start with that byte. With the magic, ambiguity is limited to raw files whose content literally starts with `AVCO\x01` — and even those fall back to raw bytes when the zstd frame or size check fails, so no legacy object can be misread. The 8-byte raw size makes `avc storage`'s compression accounting free (no decompression).
- **Compression dependency pinned to `klauspost/compress v1.17.9`** — the latest release requires Go 1.24 while CI pins 1.22; v1.17.9 keeps `go.mod` at 1.22.
- **The smoke tests surfaced and fixed two latent bugs beyond the plan:** (1) a malformed `config.toml` crashed every CLI command with a nil-pointer panic in `ensureMainBranchSetup` — it now fails with a clear ".avc/config.toml is malformed" error; (2) the same malformed config would have silently *disabled* the `[protect]` gate (fail-open) — `checkProtectedChanges` now refuses to merge when the config can't be parsed (fail-closed).
- **Export's implicit fsck was not added** — `avc export` already streams objects byte-for-byte and `avc fsck` covers auditing; wiring fsck into export remains a nice-to-have for Plan 06's remote-sync work, where transfer integrity matters more.
- **`avc_undo` joins the standard MCP tier** (now 12 tools; tier tests updated). Undoing a merge reactivates the branch *and* rebuilds its workspace from the branch HEAD — without the workspace the "active again" status would be unusable.
