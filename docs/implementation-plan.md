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
- ✅ `internal/db/db.go` — SQLite schema (projects, snapshots, files, diffs), migrations, all CRUD
- ✅ `internal/db/util.go` — `newID()`, `nowUnix()`
- ✅ `internal/fileutil/fileutil.go` — SHA256 hashing, directory walk, `.avcignore` parsing; `.git/.hg/.svn/.bzr` hardcoded exclusions
- ✅ `internal/config/config.go` — `config.toml` read/write, `.avcignore` generation (cross-stack patterns), `.gitignore` append
- ✅ `internal/snapshot/snapshot.go` — walks project, hashes files, stores blobs, inserts DB records
- ✅ `internal/restore/restore.go` — object store read-back, file write, deletion of files absent from target snapshot
- ✅ `internal/diff/diff.go` — LCS-based line counting, CRLF normalisation, `filepath.Join` for object paths, line counts for added/deleted files

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
- ✅ `internal/diff/diff.go` — LCS backtracking produces proper unified diff with `diffContextLines = 3` context and `@@ -a,b +c,d @@` headers; replaces broken multiset `buildPreview`
- ✅ "View Changes" command in sidebar wired to show diff against previous snapshot (click on any snapshot item)
- ⬜ Syntax highlighting — integrate Prism.js (loaded via CDN in Webview HTML)
- ⬜ Side-by-side layout option (currently unified only)
- ⬜ File-by-file navigation (dropdown or prev/next buttons when diff spans multiple files)
- ⬜ Copy-to-clipboard button per file diff

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
- ✅ `README.md` — quick-start guide (install, init, first snapshot, restore; CLI and extension dev setup; Windows Smart App Control note)
- ⬜ `docs/cli-reference.md` updated with Phase 4–5 commands (branch, merge)
- ⬜ `docs/architecture.md` updated with branches/merges schema section

### Release
- ⬜ Cross-platform binaries: `avc` (Linux), `avc.exe` (Windows), `avc-mac` (macOS arm64)
- ⬜ VSCode extension packaged: `vsce package` → `.vsix`
- ⬜ VSCode Marketplace listing

---

## Phase 7 — Agentic Integration

**Goal:** Make AVC the default safety layer for agent-assisted development across the major coding frameworks.

### MCP Server

- ⬜ `cmd/avc/mcp.go` — `avc mcp serve` subcommand; starts a stdio MCP server
- ⬜ `internal/mcp/server.go` — JSON-RPC 2.0 readline loop over stdio; dispatches `initialize`, `tools/list`, `tools/call`
- ⬜ `internal/mcp/tools.go` — tool registry and input schema definitions
- ⬜ `internal/mcp/handlers.go` — one function per tool; calls existing `internal/` packages directly (no CLI re-invocation)
- ⬜ Server is project-scoped — resolves `.avc/` from `cwd` at startup, same as all other commands
- ⬜ Each tool call returns the same JSON the CLI already produces — no new data layer
- ⬜ Tool results wrapped in MCP `content` envelope: `{"content": [{"type": "text", "text": "<json>"}]}`
- ⬜ Output is pretty-printed JSON for readability in agent context windows; `--compact` flag available for token-sensitive environments

### MCP Tools

| Tool | Input | Maps to |
|------|-------|---------|
| `avc_snapshot` | `label`, `agent_name?`, `notes?` | `snapshot.Create` |
| `avc_list` | *(none)* | `db.ListSnapshots` |
| `avc_diff` | `from_id`, `to_id` | `diff.Compare` |
| `avc_restore` | `id` | `restore.Restore` |
| `avc_info` | `id` | `db.GetSnapshot` + `db.GetSnapshotFiles` |
| `avc_delete` | `id` | `db.DeleteSnapshot` |

### Agent SKILLs (`avc init --skills <framework>`)

Each `--skills` flag writes two things: the MCP server config for that framework, and a behavior instruction file that tells the agent when and how to use AVC.

| Flag | MCP config written | Instruction file written |
|------|-------------------|--------------------------|
| `--skills claude-code` | `.claude/settings.json` | `.claude/commands/avc-*.md` skill files |
| `--skills cursor` | `.cursor/mcp.json` | `.cursorrules` AVC block appended |
| `--skills windsurf` | `.codeium/windsurf/mcp_config.json` | `.windsurfrules` AVC block appended |
| `--skills cline` | `.roo/mcp.json` | `.clinerules` AVC block appended |
| `--skills generic` | *(none)* | `AGENT_INSTRUCTIONS.md` drop-in prompt block |

- ⬜ All instruction files include: when to snapshot (before risky edits), when to restore (on task failure), how to read diff output
- ⬜ JSON config files (`.claude/settings.json`, `.cursor/mcp.json`, etc.) — **merge operation**: read existing file, insert `avc` entry under `mcpServers` key, write back; create file if absent; no-op if `avc` entry already present
- ⬜ Rules files (`.cursorrules`, `.clinerules`, `.windsurfrules`) — **append operation**: add AVC block after existing content, guarded by a `# AVC` marker to prevent duplicate appends on re-run
- ⬜ Multiple flags supported: `avc init --skills claude-code --skills cursor`

### Documentation

- ⬜ `docs/mcp-integration.md` — MCP server setup guide per framework
- ⬜ `docs/agent-skills.md` — what each skill file does and how to customize it

---

## Immediate next actions (Phase 3)

1. Integrate Prism.js for syntax highlighting in the diff viewer Webview
2. Add file-by-file navigation (dropdown or prev/next) when a diff spans multiple files
3. Add copy-to-clipboard button per file diff block