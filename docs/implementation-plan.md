# AVC Implementation Plan

Progress key: ✅ done · 🔧 scaffolded (code exists but incomplete) · ⬜ not started

---

## Phase 1 — Core CLI Engine

**Goal:** A working `avc` binary that agents and users can call today.

### Project scaffolding
- ✅ `go.mod` with correct module name and dependencies declared
- ✅ `go.sum` — generated via `go mod tidy`
- ✅ `main.go` entry point
- ✅ `CLAUDE.md` project instructions
- ✅ `.avcignore` default ignore patterns (at repo root)

### CLI commands (`cmd/avc/`)
- ✅ `root.go` — Cobra root, `--json` persistent flag, subcommand registration
- ✅ `helpers.go` — `requireInitializedProject()` walks up to find `.avc/`
- ✅ `init.go` — `avc init [path]`
- ✅ `snapshot.go` — `avc snapshot <label> [--agent] [--notes]`
- ✅ `list.go` — `avc list`
- ✅ `diff.go` — `avc diff <from> <to>`
- ✅ `restore.go` — `avc restore <id>`
- ✅ `info.go` — `avc info <id>`
- ✅ `delete.go` — `avc delete <id>`

### Internal packages
- ✅ `internal/db/db.go` — SQLite schema (projects, snapshots, files, diffs), migrations, all CRUD
- ✅ `internal/db/util.go` — `newID()`, `nowUnix()`
- ✅ `internal/fileutil/fileutil.go` — SHA256 hashing, directory walk, `.avcignore` parsing
- ✅ `internal/config/config.go` — `config.toml` read/write, `.avcignore` generation, `.gitignore` append
- ✅ `internal/snapshot/snapshot.go` — walks project, hashes files, stores blobs, inserts DB records
- ✅ `internal/restore/restore.go` — object store read-back and file write; `StoreObject` helper
- ✅ `internal/diff/diff.go` — two-snapshot comparison, added/modified/deleted, line count preview

### Init side effects
- ✅ Creates `.avc/` directory and `avc.db`, runs migrations
- ✅ Writes `.avc/config.toml`
- ✅ Writes `.avcignore` to project root (if absent)
- ✅ Appends `.avc/` and `.avcignore` to root `.gitignore` if one exists

### Tests
- ✅ `tests/snapshot_test.go`
- ✅ `tests/restore_test.go`
- ✅ `tests/diff_test.go`
- ✅ `tests/integration_test.go`
- ✅ `go test ./...` passes — all 13 tests green

### Phase 1 completion checklist
- ✅ `go mod tidy` — dependencies downloaded, `go.sum` generated
- ✅ `restore.StoreObject` wired into `internal/snapshot/snapshot.go`
- ✅ `cmd/avc/delete.go` implemented
- ✅ `.avcignore` written to project root on `avc init`
- ✅ `go build ./...` passes with no errors
- ✅ `go test ./...` passes with no failures

---

## Phase 2 — VSCode Extension UI

**Goal:** Non-technical users can snapshot and restore without touching the CLI.

### Extension scaffolding
- ✅ `extension/package.json` — manifest, commands, views, configuration schema
- ✅ `extension/tsconfig.json`
- ⬜ `extension/media/avc-icon.svg` — icon referenced in `package.json`, file missing
- ⬜ `npm install` run, `node_modules/` present

### Core TypeScript files
- ✅ `extension/src/cliProxy.ts` — typed wrappers for all CLI commands, `resolveProjectPath()`
- ✅ `extension/src/sidebar.ts` — `SnapshotProvider` TreeDataProvider, `SnapshotItem`
- ✅ `extension/src/extension.ts` — activation, command registration (save, refresh, restore, diff, delete)

### Extension features
- ✅ Sidebar panel registered and wired to `SnapshotProvider`
- ✅ "Save Snapshot" command with label + notes input boxes
- ✅ "Restore" command with confirmation prompt
- ✅ "Delete Snapshot" command with confirmation prompt
- ✅ Snapshot list auto-refreshes after save / restore / delete
- ✅ Each snapshot shows label, timestamp, agent name, file count (tooltip)
- ⬜ Status bar item: "AVC: X snapshots" — defined in `package.json` contributes but not implemented in `extension.ts`
- ⬜ `npm run compile` passes with no TypeScript errors
- ⬜ Manual smoke test in Extension Development Host

---

## Phase 3 — Diff Viewer

**Goal:** Users can see exactly what their agent changed, with readable syntax highlighting.

### Diff viewer
- 🔧 `extension/src/diffViewer.ts` — Webview HTML built from diff JSON; color-coded lines, file headers, line stats
- ⬜ Syntax highlighting — integrate Prism.js (loaded via CDN in Webview HTML)
- ⬜ Side-by-side layout option (currently unified only)
- ⬜ File-by-file navigation (dropdown or prev/next buttons when diff spans multiple files)
- ⬜ Copy-to-clipboard button per file diff
- ⬜ "View Changes" command in sidebar wired to show diff against previous snapshot

### Performance
- ⬜ Diff view loads in < 1s for files up to 10k lines

---

## Phase 4 — Branching (Agent Workspaces)

**Goal:** Agents work in isolated branches; main is untouched until the user approves.

### Data model
- ⬜ `branches` table: `(id, name, project_id, base_snapshot_id, created_at)`
- ⬜ `snapshots.branch_id` foreign key added via migration
- ⬜ `main` branch record created automatically on `avc init`
- ⬜ Active branch stored in `.avc/config.toml` under `[branch] active`

### CLI commands
- ⬜ `avc branch create <name> [--from <snapshot_id>]`
- ⬜ `avc branch list`
- ⬜ `avc branch switch <name>`
- ⬜ `avc branch delete <name>`
- ⬜ `avc branch diff <name>` — cumulative diff from branch point to HEAD of branch
- ⬜ `avc snapshot` respects active branch (snapshots land on the correct branch)
- ⬜ `avc restore` scoped to the branch being restored — does not affect main

### Internal packages
- ⬜ `internal/branch/branch.go` — create, list, switch, delete, resolve branch point
- ⬜ `internal/diff/diff.go` updated — accept optional branch context for cumulative diffs

### VSCode extension
- ⬜ Branch selector in sidebar header (dropdown showing all branches)
- ⬜ Snapshot list filtered to the active branch
- ⬜ "New Agent Branch" quick action
- ⬜ Branch name shown on each snapshot item when not on main

---

## Phase 5 — Merging (Controlled Integration)

**Goal:** Approved agent branches flow to main cleanly; conflicts surface clearly.

### Merge logic
- ⬜ `internal/merge/merge.go` — three-way comparison: base snapshot, main HEAD, branch HEAD
- ⬜ Clean merge: files only modified on branch → apply automatically
- ⬜ Conflict detection: files modified on both branch and main since branch point
- ⬜ Conflict markers written to working tree for conflicted files
- ⬜ `merges` table: `(id, branch_id, base_snapshot_id, merged_at, status)`; per-file rows with outcome (`clean` / `conflict` / `skipped`)
- ⬜ Auto-snapshot main before every merge (safety net — always reversible)

### CLI commands
- ⬜ `avc merge <branch_name>`
- ⬜ `avc merge <branch_name> --preview` — dry-run, lists clean/conflict files, no changes written
- ⬜ `avc merge --abort` — reverts in-progress merge to pre-merge snapshot

### VSCode extension
- ⬜ "Merge to Main" button on agent branches in sidebar
- ⬜ Conflict summary panel: lists conflicted files with "Keep Mine / Keep Agent's / View Diff" per file
- ⬜ Post-merge snapshot visible in sidebar immediately

---

## Phase 6 — Polish, Testing & Release

**Goal:** Production-ready on all four primitives.

### Testing
- ⬜ All Phase 1 unit tests passing
- ⬜ Integration test: `init → snapshot → diff → restore` round-trip
- ⬜ Integration test: `init → branch create → snapshot on branch → merge → verify main`
- ⬜ Integration test: `merge --abort` restores pre-merge state
- ⬜ VSCode extension tests (snapshot save + restore flow)
- ⬜ Race condition tests: `go test -race ./...`

### Performance benchmarks
- ⬜ Snapshot a 50 MB project: < 2s
- ⬜ Generate diff view: < 500ms
- ⬜ Restore: < 5s
- ⬜ Branch create / switch: < 100ms
- ⬜ Clean merge (no conflicts): < 3s

### Documentation
- ✅ `docs/architecture.md`
- ✅ `docs/cli-reference.md`
- ✅ `docs/contributing.md`
- ✅ `docs/project-description.md`
- ⬜ `README.md` — quick-start guide (install, init, first snapshot, restore)
- ⬜ `docs/cli-reference.md` updated with Phase 4–5 commands (branch, merge)
- ⬜ `docs/architecture.md` updated with branches/merges schema section

### Release
- ⬜ Cross-platform binaries: `avc` (Linux), `avc.exe` (Windows), `avc-mac` (macOS arm64)
- ⬜ VSCode extension packaged: `vsce package` → `.vsix`
- ⬜ VSCode Marketplace listing

---

## Immediate next actions (unblock Phase 1)

1. `go mod tidy` — generates `go.sum`, downloads `cobra`, `modernc.org/sqlite`, `BurntSushi/toml`
2. Wire `restore.StoreObject` into `internal/snapshot/snapshot.go` — without this, `avc restore` cannot retrieve file content
3. Add `cmd/avc/delete.go` — extension already calls `avc delete <id>`, CLI doesn't have it yet
4. Write `.avc/.gitignore` in `db.InitProject` or `config.WriteDefault`
5. `go build ./...` — verify it compiles
6. `go test ./...` — verify all tests pass
