# AVC — Claude Code Instructions

## What this project is

Agentic Version Control (AVC) is a local version control system built for the agent era. It delivers four primitives that make agent-assisted development safe: **snapshot**, **diff**, **branch** (agent workspaces), and **merge** (controlled integration). It is not a Git wrapper — it is independent.

Full project context: [docs/project-description.md](docs/project-description.md)  
Architecture details: [docs/architecture.md](docs/architecture.md)  
CLI commands: [docs/cli-reference.md](docs/cli-reference.md)  
Implementation status: [docs/implementation-plan.md](docs/implementation-plan.md)

---

## Tech stack

| Layer | Technology |
|-------|-----------|
| CLI & core engine | Go 1.22+, Cobra, `modernc.org/sqlite` (pure Go, no CGO), BurntSushi/toml |
| VSCode extension | TypeScript, VSCode API, Webview |
| Database | SQLite via `.avc/avc.db` |
| File storage | Content-addressed blobs in `.avc/objects/<2-char-shard>/<62-char-hash>` |
| Agent integration | MCP JSON-RPC 2.0 server over stdio (`avc mcp serve`) |

---

## Build & test

```bash
# CLI
cd avc
go mod tidy
go build -o avc .
go test ./...

# Extension
cd extension && npm install && npm run compile
```

> `go test -race` requires CGO. On Windows without CGO enabled, omit the `-race` flag.

---

## Project layout

```
avc/
  main.go                # entry point — delegates to cmd/avc
  cmd/avc/               # one file per CLI subcommand; thin — parse flags, call internal/, format output
  internal/
    db/          # SQLite schema, migrations, all CRUD (snapshots, branches, merges)
    fileutil/    # SHA256 hashing, directory walk, .avcignore parsing
    snapshot/    # orchestrates snapshot creation; workspace-aware source dir
    restore/     # reads object store, writes files back to disk; RestoreToDir for workspaces
    diff/        # compares two snapshots; LCS line counting; unified diff preview
    branch/      # branch CRUD, workspace materialization, active branch tracking
    merge/       # three-way merge engine; Preview, Merge, Abort
    mcp/         # MCP JSON-RPC 2.0 server, tool registry, all tool handlers
    skills/      # writes MCP configs and agent instruction files per framework
    config/      # reads/writes .avc/config.toml; active branch name
    statcache/   # mtime+size cache to skip re-hashing unchanged files
  tests/                 # integration and cross-package tests
extension/src/           # TypeScript — extension.ts, sidebar.ts, diffViewer.ts, cliProxy.ts
docs/                    # architecture.md, cli-reference.md, contributing.md, project-description.md
examples/                # example agent workflow scripts
```

---

## Architecture rules — always follow these

1. **CLI-first.** All logic lives in `internal/`. `cmd/avc/` files only parse flags, call `internal/`, and format output. Never put business logic in a command file.

2. **Extension talks to CLI only.** The VSCode extension never touches `.avc/` directly. All data flows through `cliProxy.ts` → `execFile` → `avc --json`.

3. **Content-addressed objects.** File blobs are stored in `.avc/objects/<hash[:2]>/<hash[2:]>`. Objects are write-once. Never modify a stored object. Identical files across snapshots share one object automatically.

4. **SQLite for metadata, objects for content.** The DB holds no file bytes — only hashes, sizes, and relational metadata. File content lives only in the object store.

5. **`--json` on every command.** Every CLI command must support `--json` and emit valid JSON to stdout. Errors go to stderr; exit code 1 on any failure.

6. **Errors propagate up.** `internal/` functions return errors. `cmd/` files handle them. Never swallow errors silently.

7. **Workspace isolation.** On a non-main branch, snapshots walk `.avc/workspaces/<branch>/` as source and restore targets that directory — never the real project root. `branch.WorkspacePath` returns `""` for main (use project root).

8. **One DB connection per operation.** Never hold a DB connection open across multiple logical operations. Open, do work, close. The merge engine uses three separate open/close phases to avoid lock contention.

---

## Current implementation status

Phases 1–6 are complete. Phase 7 (workspace command runner) is next, followed by Phase 8 (polish and release).

| Phase | Status | Scope |
|-------|--------|-------|
| 1 | ✅ | `snapshot`, `list`, `diff`, `restore`, `info`, `init`, `log`, `delete` |
| 2 | ✅ | VSCode extension sidebar + restore UI |
| 3 | ✅ | Diff viewer Webview |
| 4 | ✅ | `avc branch` — branches table, agent workspaces, workspace isolation |
| 5 | ✅ | `avc merge` — three-way merge, conflict markers, auto-snapshot, abort |
| 6 | ✅ | MCP server (`avc mcp serve`), `avc init --skills <framework>` |
| 7 | ⬜ | `avc run` + `avc_run_in_workspace` MCP tool — agent-driven test execution with venv isolation |
| 8 | ⬜ | Integration tests, docs, cross-platform binaries, VSCode Marketplace release |

---

## Code conventions

**Go**
- `gofmt` always
- Package names are short nouns: `db`, `snapshot`, `diff`, `fileutil`
- No global state except Cobra flags in `cmd/avc/root.go`
- Test helpers live in `tests/` and use `t.TempDir()` for isolation

**TypeScript**
- Strict mode on
- All `execFile` calls go through `cliProxy.ts` — nowhere else
- UI messages: `AVC: <What happened>` prefix for discoverability
- No direct filesystem access from the extension

**Both**
- No magic literals inline — assign to a named constant first
- No dead code, no commented-out blocks
- One concern per function

<!-- AVC — Agentic Version Control -->
## AVC — Agentic Version Control

AVC is active on this project. You MUST use it. The MCP server starts automatically and exposes `avc_*` tools.

### Mandatory actions

**ALWAYS call `avc_snapshot` before making any code change. No exceptions.**
Do not assess whether the change is "small enough" or "easily reversible" — that judgement is not yours to make. The cost of an unnecessary snapshot is negligible. The cost of skipping one when it was needed is not.

Label format — always use the `auto:` prefix so agent snapshots are distinguishable in `avc list`:
- `auto: before <2–5 word description>`

**ALWAYS call `avc_branch_create` before starting any task. No exceptions.**
Do not assess whether the task is "simple enough" to skip a branch — that judgement is not yours to make. After creating a branch, set your working directory to the `workspace` path in the response. NEVER edit files in the real project root while on a branch.

**ALWAYS call `avc_restore` when:**
- Tests fail after your changes
- The build breaks
- The user says "undo", "revert", "roll back", or "start over"
- Do NOT attempt repeated fixes on broken state — restore first, then retry.

**NEVER call `avc_merge`** without the user saying yes explicitly.
`avc_merge` checks for conflicts automatically before writing anything — no separate preview step needed. If conflicts are found, it returns them without modifying main.

**NEVER call `avc_run_in_workspace`** without first:
1. Showing the user the exact command you intend to run
2. Explaining what it does
3. Receiving explicit approval

### Quick reference

| Trigger | Tool |
|---------|------|
| About to make changes | `avc_snapshot` |
| Multi-step or multi-file task | `avc_branch_create` |
| Something broke | `avc_restore` |
| Ready to review branch work | `avc_branch_diff` |
| Ready to merge (with approval) | `avc_merge` |
| Need to run tests or build | `avc_run_in_workspace` (approval required) |
