# Plan 06 — Ecosystem & Adoption (Tier C)

**Covers review items:** C1 (git bridge), C2 (remote sync), C3 (MCP modernization), C4 (web cockpit)
**Goal:** AVC stops competing with the tools teams already use and becomes a layer on top of them.
**Prerequisite:** Plans 01–05 complete (C4 additionally requires Plan 03·5f web hardening; C1/C2 build on Plan 04 A4's `objstore`).
**Estimated duration:** ~4 weeks

**Suggested order within the plan:** C3 → C1 → C4 → C2. C3 is smallest and improves every existing agent integration immediately; C1 is the adoption unlock; C2 is the most infrastructure-heavy and benefits from C1's serialization work.

---

## C3 · MCP modernization

**Priority:** P1 · **Effort:** M
**Files:** `avc/internal/mcp/server.go`, `tools.go`, `handlers.go`, `instructions.go`, `avc/internal/skills/skills.go`

### Problem

The server speaks protocol rev `2024-11-05`. Every tool result is JSON serialized into a text block that agents must re-parse; snapshots/diffs aren't browsable without tool calls; and the "user must approve merge" rule is an instruction agents can ignore rather than a protocol-level interaction.

### Implementation

**Step 1 — protocol rev + capability negotiation.** Target the current spec revision (2025-06-18 or later at implementation time). Honor the client's `protocolVersion` in `initialize` and degrade gracefully — existing clients keep working exactly as today.

**Step 2 — structured tool output.** Add `outputSchema` to each tool definition and return `structuredContent` alongside the existing text block (the spec requires text fallback anyway, so this is purely additive). The typed structs in `handlers.go` already exist — this is mostly annotation work. Biggest wins: `avc_status`, `avc_list`, `avc_merge` (agents branch on `conflicts > 0` without parsing).

**Step 3 — resources.** Advertise the `resources` capability:

- `avc://snapshots` — list (paginated)
- `avc://snapshot/<id>` — metadata + file list
- `avc://snapshot/<id>/file/<path>` — file content at snapshot (the `cat` machinery)
- `avc://diff/<from>/<to>` — rendered diff
- `avc://timeline/<session>` — Plan 05 B3 timeline

Read-only browsing stops consuming tool-call turns.

**Step 4 — elicitation for merge approval.** Where the client advertises elicitation support, `avc_merge` (and `avc_merge_train`) triggers a protocol-level confirmation prompt answered by the *human*, making "never merge without explicit approval" mechanically enforced — the `run.enabled` philosophy applied to merge. No elicitation support → current behavior (instruction-based) with the tool description unchanged.

**Step 5 —** regenerate `internal/skills` templates to mention resources and structured output; bump `serverVersion`.

### Tests

Golden-file JSON-RPC tests per protocol rev (old client / new client). Structured + text content both present and consistent. Resource reads match tool-call equivalents. Elicitation flow: accept → merge proceeds; decline → merge not executed.

---

## C1 · Git bridge

**Priority:** P0 (biggest adoption unlock) · **Effort:** L
**Files:** new `avc/internal/gitbridge/`, new `avc/cmd/avc/git.go`

### Problem

Teams live in git. As long as AVC history is invisible to git tooling, adopting AVC means "another VCS" instead of "a safety layer on what you already use."

### Design

Shadow-branch export, plumbing-first (shell out to `git hash-object -w`, `mktree`, `commit-tree`, `update-ref` — no CGO, no reimplementing git):

```
avc git sync [--branch <avc-branch>] [--ref refs/avc/<branch>]
avc git sync --watch          # after each snapshot (post-snapshot hook wiring)
avc git import --commit <sha> # snapshot a git commit's tree as an AVC baseline
```

- **Mapping:** one AVC snapshot → one git commit on `refs/avc/<branch>`; parent = previous synced snapshot. Commit message = label; trailers carry identity: `AVC-Snapshot-ID`, `AVC-Agent`, `AVC-Session` (B3), notes in the body.
- **State:** `git_sync` table (`snapshot_id ↔ commit_sha`) makes sync incremental and idempotent — re-running syncs only new snapshots.
- **Requirements:** a git repo at the project root and `git` on PATH; clear error otherwise. Never touches the user's checked-out branches, index, or working tree — refs under `refs/avc/` only. Pushing the ref (`git push origin refs/avc/main`) makes agent history reviewable in any git host UI; document, don't automate.
- **Import:** `avc git import` reads a commit's tree (`git ls-tree -r` + `cat-file`) into the object store and creates a snapshot labeled `git: <short-sha> <subject>` — instant AVC baselines for existing repos, and the down-payment on future bidirectional flows.
- **Non-goals (this plan):** exporting AVC *merges* as git merges, syncing to the user's working branches, and any automatic push.

### Tests

Round-trip: snapshots → sync → `git ls-tree` matches snapshot file lists, trailers parse, incremental re-sync adds only new commits. Import: git commit → snapshot → `avc restore` reproduces the tree byte-identically. Repo-less project → clean error. Deleted-file snapshots sync correctly (tree omits them).

---

## C4 · Web UI as the merge cockpit

**Priority:** P1 · **Effort:** L
**Files:** `avc/internal/web/` (server + static), `avc/internal/api/api.go`

### Problem

Non-VSCode users have no good surface for the two human moments AVC creates: reviewing what an agent did, and resolving a conflicted merge. The web UI (hardened in Plan 03·5f) has the endpoints but not the workflow.

### Implementation

All server logic via `internal/api` (architecture rule A1) — new endpoints are thin.

**Step 1 — conflict resolution view.** For a merge with conflicts, per file render ours/base/theirs three-pane (content via existing `merge_files` hashes + object reads; new `GET /api/merge/<id>/file?path=…` returns all three). Actions per file: accept ours / accept theirs / edit merged text — `POST /api/merge/resolve` wraps the existing `merge.ResolveFile`. A "conflicts remaining" counter driven by `ListConflicts`; when zero, a "complete merge" button re-invokes merge.

**Step 2 — timeline + review view.** Render Plan 05 B3's `/api/timeline`: sessions → tasks → snapshots with change summaries; click-through to snapshot diff (existing diff endpoints). This is the "review the agent's night shift" page and the reason a non-VSCode user opens the UI at all.

**Step 3 — merge-train dashboard.** For `avc merge --train` runs: per-branch status stream (poll `GET /api/merge/train/<id>`), stop-on-conflict state clearly shown with a link into the Step 1 resolver.

**Step 4 — approval affordances.** Merge/restore buttons show the Plan 04 safety context: which pre-merge/pre-restore snapshot protects the action, and the protected-paths verdict, before the confirm click.

*(Front-end stays framework-free embedded static assets, matching the current approach — no build pipeline added.)*

### Tests

API-level: three-pane payload correct for each conflict class (including delete-vs-edit); resolve endpoints round-trip; auth + Origin checks from Plan 03 hold on every new endpoint. Manual test script for the UI flows in `docs/` (webview E2E automation is out of scope).

---

## C2 · Remote sync

**Priority:** P2 · **Effort:** XL
**Files:** new `avc/internal/remote/` (+ backends), new `avc/cmd/avc/push.go` / `pull.go`, `avc/internal/config/config.go`

### Problem

History is machine-local. Agent work on CI runners evaporates; teammates can't see each other's agent history; there is no offsite copy.

### Design

Content-addressed stores make sync embarrassingly parallelizable: push = upload missing objects + metadata delta; pull = the reverse.

```toml
[remote]
url = "s3://bucket/prefix"     # or "ssh://host/path"
```

```
avc push [--branch <name>]
avc pull [--branch <name>]
avc remote status
```

- **Layout on the remote:** `objects/<shard>/<hash>` (v2 format from A4 — compressed on the wire for free) plus `meta/<generation>.avcmeta`, a metadata bundle reusing the export/import serialization (branches, snapshots, files, tags, sessions).
- **Sync model — additive, not collaborative:** objects are immutable and dedupe by name; metadata merges by ID with re-generation on conflict, exactly like `avc import`. Two machines pushing the same branch name produce two branches (`<name>` and `<name>@<host>`) rather than attempting distributed consensus. Snapshot-history sharing, not a distributed VCS — state that limit in the docs.
- **Backends:** interface `List/Get/Put(hash)` + metadata get/put. Ship `s3` (AWS SDK v2; covers R2/MinIO via endpoint override) and `ssh` (sftp). `file://` backend for tests.
- **Integrity:** verify hashes on pull (fsck logic); never overwrite an existing local object.
- **CI story (the killer demo):** runner does `avc pull` → agents work with full history → `avc push` persists everything the fleet did. Document as a first-class recipe.

### Tests

`file://` round-trip: push from A, pull into fresh B, all snapshots restorable, fsck clean. Incremental push uploads only new objects (count assertion). Same-name branch collision → suffixed branch. Interrupted push (injected backend failure) → re-push completes; remote never has torn objects (metadata written last).

---

## Exit criteria

- [ ] MCP: structured output on all tools, resources browsable, elicitation-gated merge on supporting clients, old clients unaffected (golden tests)
- [ ] `avc git sync` produces a shadow ref whose commits match snapshots (trees + trailers); incremental; import round-trips
- [ ] Web cockpit: conflicts resolvable end-to-end from the browser; timeline view live; all new endpoints behind token + Origin checks
- [ ] `avc push`/`pull` round-trip via `file://` and one real backend; pull verifies hashes; CI recipe documented
- [ ] `go test ./...` green; `docs/cli-reference.md`, `docs/architecture.md`, and README updated — with this plan complete, the review's roadmap (S→T→U→V) is fully delivered
