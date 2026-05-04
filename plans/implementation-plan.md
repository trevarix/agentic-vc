# AVC Implementation Plan

Progress key: ✅ done · 🔧 scaffolded (code exists but incomplete) · ⬜ not started

---

## Phase 1 — Core CLI Engine

**Goal:** A working `avc` binary that agents and users can call today.

### Project scaffolding
- ✅ `avc/go.mod` with correct module name and dependencies declared
- ✅ `avc/go.sum` — generated via `go mod tidy`
- ✅ `avc/main.go` entry point
- ✅ `CLAUDE.md` project instructions
- ✅ `.avcignore` default ignore patterns (at repo root)

### CLI commands (`avc/cmd/avc/`)
- ✅ `root.go` — Cobra root, `--json` persistent flag, subcommand registration
- ✅ `helpers.go` — `requireInitializedProject()` walks up to find `.avc/`
- ✅ `color.go` — ANSI color helpers, terminal detection, `NO_COLOR` support
- ✅ `init.go` — `avc init [path]`
- ✅ `snapshot.go` — `avc snapshot <label> [--agent] [--notes]`
- ✅ `list.go` — `avc list`
- ✅ `diff.go` — `avc diff <from> <to>`
- ✅ `restore.go` — `avc restore <id>`
- ✅ `info.go` — `avc info <id>`
- ✅ `delete.go` — `avc delete <id>`
- ✅ `log.go` — `avc log` tree diagram of snapshot history

### Internal packages
- ✅ `avc/internal/db/db.go` — SQLite schema (projects, snapshots, files, diffs), migrations, all CRUD
- ✅ `avc/internal/db/util.go` — `newID()`, `nowUnix()`
- ✅ `avc/internal/fileutil/fileutil.go` — SHA256 hashing, directory walk, `.avcignore` parsing; `.git/.hg/.svn/.bzr` hardcoded exclusions
- ✅ `avc/internal/config/config.go` — `config.toml` read/write, `.avcignore` generation (cross-stack patterns), `.gitignore` append
- ✅ `avc/internal/snapshot/snapshot.go` — walks project, hashes files, stores blobs, inserts DB records
- ✅ `avc/internal/restore/restore.go` — object store read-back, file write, deletion of files absent from target snapshot
- ✅ `avc/internal/diff/diff.go` — LCS-based line counting, CRLF normalisation, `filepath.Join` for object paths, line counts for added/deleted files

### Init side effects
- ✅ Creates `.avc/` directory and `avc.db`, runs migrations
- ✅ Writes `.avc/config.toml`
- ✅ Writes `.avcignore` to project root (if absent) with cross-stack patterns
- ✅ Appends `.avc/` and `.avcignore` to root `.gitignore` if one exists

### Bug fixes (resolved during testing)
- ✅ Diff line counts wrong — set-based approach replaced with LCS algorithm
- ✅ Objects not found on Windows — `fmt.Sprintf` path replaced with `filepath.Join`
- ✅ Mixed CRLF/LF line endings counted as different lines — normalised in `splitLines`
- ✅ Added/deleted files always showed `+0 -0` — `enrichWithLineCounts` now called for all change types
- ✅ Restore left behind files added after the target snapshot — restore now deletes tracked files absent from the target
- ✅ `.git/` and other VCS dirs tracked by AVC — hardcoded exclusions in `WalkProject`
- ✅ `.venv/` and other stack-specific dirs tracked — expanded default `.avcignore`

### Tests
- ✅ `avc/tests/snapshot_test.go`
- ✅ `avc/tests/restore_test.go`
- ✅ `avc/tests/diff_test.go`
- ✅ `avc/tests/integration_test.go`
- ✅ `go test ./...` passes — all 13 tests green

### Phase 1 completion checklist
- ✅ `go mod tidy` — dependencies downloaded, `avc/go.sum` generated
- ✅ `restore.StoreObject` wired into `avc/internal/snapshot/snapshot.go`
- ✅ `avc/cmd/avc/delete.go` implemented
- ✅ `.avcignore` written to project root on `avc init`
- ✅ `go build ./...` passes with no errors
- ✅ `go test ./...` passes with no failures

---

## Phase 2 — VSCode Extension UI

**Goal:** Non-technical users can snapshot and restore without touching the CLI.

### Extension scaffolding
- ✅ `extension/package.json` — manifest, commands, views, configuration schema
- ✅ `extension/tsconfig.json` — `"types": ["node"]` added to resolve `child_process` types
- ✅ `extension/media/avc-icon.svg` — camera icon for activity bar
- ✅ `npm install` run, `node_modules/` present

### Core TypeScript files
- ✅ `extension/src/cliProxy.ts` — typed wrappers for all CLI commands, `resolveProjectPath()`; `cwd` passed to `execFile` (replaced broken `process.chdir`)
- ✅ `extension/src/sidebar.ts` — `SnapshotProvider` TreeDataProvider, `SnapshotItem`
- ✅ `extension/src/extension.ts` — activation, command registration (save, refresh, restore, diff, delete)

### Extension features
- ✅ Sidebar panel registered and wired to `SnapshotProvider`
- ✅ "Save Snapshot" command with label + notes input boxes
- ✅ "Restore" command with confirmation prompt
- ✅ "Delete Snapshot" command with confirmation prompt
- ✅ Snapshot list auto-refreshes after save / restore / delete
- ✅ Each snapshot shows label, timestamp, agent name, file count (tooltip)
- ✅ Status bar item: "AVC: X snapshots" — updates after every save / restore / delete
- ✅ `npm run compile` passes with no TypeScript errors
- ✅ Manual smoke test in Extension Development Host

---

## Phase 3 — Diff Viewer

**Goal:** Users can see exactly what their agent changed, with readable syntax highlighting.

### Diff viewer
- ✅ `extension/src/diffViewer.ts` — table-based unified diff; actual file line numbers, green/red row highlights, muted context lines, `@@` hunk headers
- ✅ `avc/internal/diff/diff.go` — LCS backtracking produces proper unified diff with `diffContextLines = 3` context and `@@ -a,b +c,d @@` headers; replaces broken multiset `buildPreview`
- ✅ "View Changes" command in sidebar wired to show diff against previous snapshot (click on any snapshot item)
- ✅ Syntax highlighting — Prism.js loaded via CDN; language auto-detected from file extension; `<code class="language-*">` per cell; autoloader fetches grammars on demand
- ⬜ Side-by-side layout option (currently unified only)
- ✅ File-by-file navigation — sticky jump-link nav bar rendered when diff spans multiple files; each file block has anchor `#file-N`
- ✅ Copy-to-clipboard button per file diff — "Copy" button in file header; flashes "Copied!" for 2s; writes raw unified diff text to clipboard

### Performance
- ⬜ Diff view loads in < 1s for files up to 10k lines

---

## Phase 4 — Branching (Agent Workspaces)

**Goal:** Agents work in isolated branches; main is untouched until the user approves.

### Data model
- ✅ `branches` table: `(id, name, project_id, base_snapshot_id, created_at)`
- ✅ `snapshots.branch_id` foreign key added via migration
- ✅ `main` branch record created automatically on `avc init`
- ✅ Active branch stored in `.avc/config.toml` under `[branch] active`

### CLI commands
- ✅ `avc branch create <name> [--from <snapshot_id>]`
- ✅ `avc branch list`
- ✅ `avc branch switch <name>`
- ✅ `avc branch delete <name>`
- ✅ `avc branch diff <name>` — cumulative diff from branch point to HEAD of branch
- ✅ `avc snapshot` respects active branch (snapshots land on the correct branch)
- ✅ `avc restore` scoped to the branch being restored — does not affect main

### Internal packages
- ✅ `avc/internal/branch/branch.go` — create, list, switch, delete, resolve branch point
- ✅ `avc/internal/config/config.go` — `BranchConfig`, `SetActiveBranch`, `Save`
- ✅ `avc/internal/db/db.go` — `EnsureMainBranch`, `ListSnapshotsByBranch`, `GetHeadSnapshot`, branch CRUD

### Implementation note — branch creation must not snapshot
`avc branch create` records `base_snapshot_id` in the `branches` table and writes
the active branch to `.avc/config.toml`. It must **not** take an automatic snapshot
of all project files to establish the branch point. The base snapshot already exists
and its file records already reference the correct objects — inheriting by ID is
instant and storage-free. Taking a new snapshot here would write 1,000 DB rows and
trigger 1,000 `StoreObject` calls (all no-ops, but still unnecessary) for every
branch creation regardless of project size.

### VSCode extension
- ✅ Branch status bar item (shows active branch, click to switch)
- ✅ Snapshot list filtered to the active branch (via `avc list --json`)
- ✅ "Create Branch" button in sidebar view title
- ✅ `avc.createBranch`, `avc.switchBranch`, `avc.deleteBranch` commands registered
- ✅ Branch selector via QuickPick on switch command
- ⬜ Branch name shown on each snapshot item when not on main

---

## Phase 4.5 — Workspace Isolation

**Goal:** Agent branches operate on a materialized copy of the project, not the real
working directory. Main is provably untouched until merge.

Full design: [docs/workspace-isolation.md](workspace-isolation.md)

### Internal packages
- ✅ `restore.RestoreToDir(projectRoot, snapshotID, targetDir)` — restore to any directory, not just project root
- ✅ `snapshot.Create` accepts `sourceDir` param — walk workspace instead of project root
- ✅ `branch.WorkspacePath(projectRoot, branchName)` — derives `.avc/workspaces/<name>/`
- ✅ `branch.MaterializeWorkspace(projectRoot, branch)` — populates workspace from base snapshot
- ✅ `branch.RemoveWorkspace(projectRoot, branchName)` — cleanup on delete
- ✅ `statcache.Empty()` exported for workspace snapshot path (no cache contamination)

### CLI changes
- ✅ `avc branch create` — materializes workspace after creating branch record; prints workspace path
- ✅ `avc branch delete` — removes workspace directory
- ✅ `avc snapshot` on non-main branch — walks workspace dir as source
- ✅ `avc restore` on non-main branch — writes to workspace dir as target

### Tests
- ✅ `avc/tests/workspace_test.go` — 6 tests: materialize, hardlink inode, snapshot targets workspace, restore targets workspace, delete removes workspace, main has no workspace

### VSCode extension
- ✅ `Branch.workspace` field added to `cliProxy.ts`
- ✅ Workspace path shown in "Branch created" notification

---

## Phase 5 — Merging (Controlled Integration) ✅

**Goal:** Approved agent branches flow to main cleanly; conflicts surface clearly.

### Merge logic
- ✅ `avc/internal/merge/merge.go` — three-way comparison: base snapshot, main HEAD, branch HEAD
- ✅ Clean merge: files only modified on branch → applied automatically
- ✅ Conflict detection: files modified on both branch and main since branch point
- ✅ Conflict markers written to working tree for conflicted files (`<<<<<<< main / ======= / >>>>>>> branch`)
- ✅ `merges` + `merge_files` tables in `db.go`; per-file rows with decision (`clean` / `conflict` / `skipped`)
- ✅ Auto-snapshot main before every merge (fully reversible via `--abort`)

### CLI commands
- ✅ `avc merge <branch_name>`
- ✅ `avc merge <branch_name> --preview` — dry-run, lists clean/conflict files, no changes written
- ✅ `avc merge --abort` — reverts in-progress merge to pre-merge snapshot

### MCP tools
- ✅ `avc_merge_preview` — preview merge decisions without writing files
- ✅ `avc_merge` — perform three-way merge; returns per-file decisions
- ✅ `avc_merge_abort` — restore main from pre-merge auto-snapshot

### VSCode extension
- ✅ "Merge Branch to Main" button in sidebar toolbar (`$(git-merge)` icon)
- ✅ Preview modal before committing — shows clean/conflict/skipped counts
- ✅ Post-merge warning if conflicts present; "Abort Merge" command available
- ✅ `avc.mergeBranch` and `avc.abortMerge` registered commands

---

## Phase 6 — Agentic Integration

**Goal:** Make AVC the default safety layer for agent-assisted development across the major coding frameworks.

### MCP Server

- ✅ `avc/cmd/avc/mcp.go` — `avc mcp serve` subcommand; starts a stdio MCP server
- ✅ `avc/internal/mcp/server.go` — JSON-RPC 2.0 readline loop over stdio; dispatches `initialize`, `tools/list`, `tools/call`, `ping`
- ✅ `avc/internal/mcp/tools.go` — tool registry and input schema definitions
- ✅ `avc/internal/mcp/handlers.go` — one function per tool; calls existing `internal/` packages directly (no CLI re-invocation)
- ✅ Server is project-scoped — resolves `.avc/` from `cwd` at startup, same as all other commands
- ✅ Each tool call returns the same JSON the CLI already produces — no new data layer
- ✅ Tool results wrapped in MCP `content` envelope: `{"content": [{"type": "text", "text": "<json>"}]}`
- ✅ Output is pretty-printed JSON for readability in agent context windows; `--compact` flag available for token-sensitive environments

### MCP Tools

| Tool | Input | Maps to |
|------|-------|---------|
| `avc_snapshot` | `label`, `agent_name?`, `notes?` | `snapshot.Create` (workspace-aware) |
| `avc_list` | *(none)* | `db.ListSnapshotsByBranch` |
| `avc_diff` | `from_id`, `to_id` | `diff.Compare` |
| `avc_restore` | `id` | `restore.RestoreToDir` (workspace-aware) |
| `avc_info` | `id` | `db.GetSnapshot` + `db.GetSnapshotFiles` |
| `avc_delete` | `id` | `db.DeleteSnapshot` |
| `avc_branch_create` | `name`, `from_snapshot_id?` | `branch.Create` + auto-switch |
| `avc_branch_list` | *(none)* | `branch.List` |
| `avc_branch_switch` | `name` | `branch.Switch` |
| `avc_branch_diff` | `name` | `diff.Compare` (base → HEAD) |

### Agent SKILLs (`avc init --skills <framework>`)

Each `--skills` value writes two things: the MCP server config for that framework, and a behavior instruction file that tells the agent when and how to use AVC.
Accepts a comma-separated list: `avc init --skills claude-code,cursor,windsurf`

| Flag | MCP config written | Instruction file written |
|------|-------------------|--------------------------|
| `claude-code` | `.claude/settings.json` | `.claude/skills/avc-*/SKILL.md` (4 skill files) |
| `cursor` | `.cursor/mcp.json` | `.cursor/rules/avc.mdc` |
| `windsurf` | `.codeium/windsurf/mcp_config.json` | `.windsurfrules` AVC block appended |
| `generic` | *(none)* | `AGENT_INSTRUCTIONS.md` drop-in prompt block |

- ✅ All instruction files include: when to snapshot, when to restore, branch workflow, merge approval rules
- ✅ JSON config files — **merge operation**: reads existing file, inserts `avc` entry under `mcpServers`, writes back; creates file if absent; no-op if `avc` entry already present
- ✅ Rules files — **append operation**: adds AVC block after existing content, guarded by `# AVC` marker to prevent duplicate appends on re-run
- ✅ Comma-separated list: `avc init --skills claude-code,cursor`
- ✅ `avc/internal/skills/skills.go` — all framework logic isolated in one package

### Documentation

- ⬜ `docs/mcp-integration.md` — MCP server setup guide per framework
- ⬜ `docs/agent-skills.md` — what each skill file does and how to customize it

---

## Phase 7 — Polish, Testing & Release

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
- ✅ `README.md` — quick-start guide (install, init, first snapshot, restore; CLI and extension dev setup; Windows Smart App Control note)
- ⬜ `docs/cli-reference.md` updated with Phase 4–5 commands (branch, merge)
- ⬜ `docs/architecture.md` updated with branches/merges schema section

### Release
- ⬜ Cross-platform binaries: `avc` (Linux), `avc.exe` (Windows), `avc-mac` (macOS arm64)
- ⬜ VSCode extension packaged: `vsce package` → `.vsix`
- ⬜ VSCode Marketplace listing

---

## Immediate next actions (Phase 7)

1. Integration tests: `init → branch → snapshot → merge → verify main` round-trip
2. Integration test: `merge --abort` restores pre-merge state
3. Update `docs/cli-reference.md` with Phase 4–6 commands (branch, merge, mcp, init --skills)
4. Update `docs/architecture.md` with branches/merges schema section
5. Cross-platform binary builds + VSCode extension packaging